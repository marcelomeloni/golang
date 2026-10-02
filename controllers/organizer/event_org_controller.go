package organizer

import (

	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/internal/dbutil"
	"bilheteria-api/services/eventservice"
	"bilheteria-api/services/geocoding"
	"bilheteria-api/services/orgservice"
	"bilheteria-api/services/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SaveDraftHandler — POST /org/:slug/events
func SaveDraftHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var body SaveDraftRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title é obrigatório para salvar rascunho"})
		return
	}

	eventSlug, err := eventservice.GenerateUniqueSlug(ctx, db, body.Title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar slug"})
		return
	}

	startDate, endDate, err := parseDatePair(body.StartDate, body.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	locationJSON, _ := json.Marshal(body.Location)
	requirementsJSON, _ := json.Marshal(body.Requirements)

	// Geocodifica a cidade em background — falha silenciosa se não encontrar
	var coords *geocoding.Coordinates
	if body.Location != nil {
		coords, _ = geocoding.FromCityState(ctx, body.Location.City, body.Location.State)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	eventID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO events
           (id, organization_id, title, slug, description, category,
            instagram, status, start_date, end_date, location, requirements, geolocation)
         VALUES ($1,$2,$3,$4,$5,$6,$7,'draft',$8,$9,$10,$11,
           CASE WHEN $12::float8 IS NOT NULL
                THEN ST_SetSRID(ST_MakePoint($13::float8, $12::float8), 4326)
                ELSE NULL END
         )`,
		eventID, orgID,
		body.Title, eventSlug,
		dbutil.NullableText(body.Description),
		dbutil.NullableText(body.Category),
		dbutil.NullableText(body.Instagram),
		startDate, endDate,
		dbutil.NullableJSON(locationJSON),
		dbutil.NullableJSON(requirementsJSON),
		coordLat(coords), coordLng(coords),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar rascunho: " + err.Error()})
		return
	}

	if len(body.Categories) > 0 {
		if err := eventservice.InsertTicketCategories(ctx, tx, eventID, body.Categories); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar categorias: " + err.Error()})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"event_id": eventID,
		"slug":     eventSlug,
		"status":   "draft",
		"message":  "rascunho salvo",
	})
}

// UpdateEventHandler — PATCH /org/:slug/events/:id
func UpdateEventHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	if _, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var body UpdateEventRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	startDate, endDate, err := parseDatePairPtr(body.StartDate, body.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	locationJSON, _ := json.Marshal(body.Location)
	requirementsJSON, _ := json.Marshal(body.Requirements)

	// Regeocodifica só se a localização foi enviada nesta requisição
	var coords *geocoding.Coordinates
	if body.Location != nil {
		coords, _ = geocoding.FromCityState(ctx, body.Location.City, body.Location.State)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE events
            SET title        = COALESCE($1, title),
                description  = COALESCE($2, description),
                category     = COALESCE($3, category),
                instagram    = COALESCE($4, instagram),
                start_date   = COALESCE($5, start_date),
                end_date     = COALESCE($6, end_date),
                location     = COALESCE($7, location),
                requirements = COALESCE($8, requirements),
                geolocation  = CASE
                                 WHEN $9::float8 IS NOT NULL
                                 THEN ST_SetSRID(ST_MakePoint($10::float8, $9::float8), 4326)
                                 ELSE geolocation
                               END,
                updated_at   = now()
          WHERE id = $11`,
		body.Title, body.Description, body.Category, body.Instagram,
		startDate, endDate,
		dbutil.NullableJSON(locationJSON),
		dbutil.NullableJSON(requirementsJSON),
		coordLat(coords), coordLng(coords),
		eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar evento: " + err.Error()})
		return
	}

	if len(body.Categories) > 0 {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM ticket_categories WHERE event_id = $1`, eventID,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover categorias antigas"})
			return
		}
		if err := eventservice.InsertTicketCategories(ctx, tx, eventID, body.Categories); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao recriar categorias: " + err.Error()})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "evento atualizado"})
}

// PublishEventHandler — PATCH /org/:slug/events/:id/publish
func PublishEventHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	if _, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	snap, err := fetchEventSnapshot(ctx, db, eventID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	switch snap.Status {
	case "published":
		c.JSON(http.StatusConflict, gin.H{"error": "evento já está publicado"})
		return
	case "cancelled":
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "não é possível publicar um evento cancelado"})
		return
	}

	if errs := validateForPublish(snap); len(errs) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":  "o evento não pode ser publicado",
			"fields": errs,
		})
		return
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE events SET status = 'published', updated_at = now() WHERE id = $1`, eventID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao publicar evento"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "evento publicado com sucesso"})
}

// CancelEventHandler — PATCH /org/:slug/events/:id/cancel
func CancelEventHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var status string
	var ticketsSold int
	err = db.QueryRowContext(ctx,
		`SELECT e.status, COUNT(t.id)
           FROM events e
           LEFT JOIN orders o  ON o.event_id = e.id AND o.status = 'paid'
           LEFT JOIN tickets t ON t.order_id = o.id
          WHERE e.id = $1 AND e.organization_id = $2
          GROUP BY e.status`,
		eventID, orgID,
	).Scan(&status, &ticketsSold)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	switch status {
	case "cancelled":
		c.JSON(http.StatusConflict, gin.H{"error": "evento já está cancelado"})
		return
	case "finished":
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "não é possível cancelar um evento encerrado"})
		return
	}

	if ticketsSold > 0 {
		role, _ := orgservice.GetMemberRole(ctx, db, orgSlug, uid)
		if role != "owner" {
			c.JSON(http.StatusForbidden, gin.H{"error": "este evento tem ingressos vendidos. Apenas o owner pode cancelá-lo."})
			return
		}
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE events SET status = 'cancelled', updated_at = now() WHERE id = $1`, eventID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar evento"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "evento cancelado", "tickets_sold": ticketsSold})
}

// UploadEventBannerHandler — POST /org/:slug/events/:id/banner
func UploadEventBannerHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	if _, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "arquivo não encontrado no campo 'file'"})
		return
	}
	defer file.Close()

	result, err := storage.UploadOrgImage(file, header, "event-banners")
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE events SET image_url = $1, updated_at = now() WHERE id = $2`,
		result.URL, eventID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar banner"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": result.URL})
}

// ─── Helpers privados ─────────────────────────────────────────────────────────

type eventSnapshot struct {
	Status       string
	Title        *string
	Category     *string
	ImageURL     *string
	StartDate    *time.Time
	EndDate      *time.Time
	LocationCity *string
	Requirements []byte
	BatchCount   int
}

func fetchEventSnapshot(ctx context.Context, db *sql.DB, eventID string) (*eventSnapshot, error) {
	var snap eventSnapshot
	err := db.QueryRowContext(ctx,
		`SELECT
           e.status, e.title, e.category, e.image_url,
           e.start_date, e.end_date,
           e.location->>'city' AS location_city,
           e.requirements,
           COUNT(tb.id) AS batch_count
          FROM events e
          LEFT JOIN ticket_categories tc ON tc.event_id = e.id
          LEFT JOIN ticket_batches tb    ON tb.category_id = tc.id AND tb.status = 'active'
         WHERE e.id = $1
         GROUP BY e.status, e.title, e.category, e.image_url,
                  e.start_date, e.end_date, e.location, e.requirements`,
		eventID,
	).Scan(
		&snap.Status, &snap.Title, &snap.Category,
		&snap.ImageURL, &snap.StartDate, &snap.EndDate,
		&snap.LocationCity, &snap.Requirements, &snap.BatchCount,
	)
	return &snap, err
}

func validateForPublish(snap *eventSnapshot) []string {
	var errs []string

	if snap.Title == nil || strings.TrimSpace(*snap.Title) == "" {
		errs = append(errs, "título obrigatório")
	}
	if snap.Category == nil || *snap.Category == "" {
		errs = append(errs, "categoria do evento obrigatória")
	}
	if snap.ImageURL == nil {
		errs = append(errs, "banner do evento obrigatório")
	}
	if snap.StartDate == nil {
		errs = append(errs, "data de início obrigatória")
	}
	if snap.EndDate == nil {
		errs = append(errs, "data de término obrigatória")
	}
	if snap.StartDate != nil && snap.EndDate != nil && !snap.EndDate.After(*snap.StartDate) {
		errs = append(errs, "data de término deve ser posterior ao início")
	}
	if snap.LocationCity == nil || *snap.LocationCity == "" {
		errs = append(errs, "localização (cidade) obrigatória")
	}
	if snap.BatchCount == 0 {
		errs = append(errs, "pelo menos um lote de ingressos ativo é obrigatório")
	}

	var req EventRequirementsInput
	if snap.Requirements != nil {
		if err := json.Unmarshal(snap.Requirements, &req); err == nil && !req.AcceptedTerms {
			errs = append(errs, "termos de uso não aceitos")
		}
	} else {
		errs = append(errs, "termos de uso não aceitos")
	}

	return errs
}

func parseDatePair(start, end string) (*time.Time, *time.Time, error) {
	s, err := parseOptionalTime(start)
	if err != nil {
		return nil, nil, err
	}
	e, err := parseOptionalTime(end)
	if err != nil {
		return nil, nil, err
	}
	return s, e, nil
}

func parseDatePairPtr(start, end *string) (*time.Time, *time.Time, error) {
	var s, e string
	if start != nil {
		s = *start
	}
	if end != nil {
		e = *end
	}
	return parseDatePair(s, e)
}

func parseOptionalTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// coordLat e coordLng retornam nil quando não há coordenadas,
// permitindo o CASE WHEN $n IS NOT NULL no SQL.
func coordLat(c *geocoding.Coordinates) interface{} {
	if c == nil {
		return nil
	}
	return c.Lat
}

func coordLng(c *geocoding.Coordinates) interface{} {
	if c == nil {
		return nil
	}
	return c.Lng
}