package organizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/internal/dbutil"
	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/eventservice"
	"bilheteria-api/services/geocoding"
	"bilheteria-api/services/orderservice"
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

	var locationJSON []byte
	if body.Location != nil && !body.Location.IsEmpty() {
		locationJSON = marshalJSON(body.Location)
	}
	requirementsJSON := marshalJSON(body.Requirements)

	// Geocodifica a cidade em background — falha silenciosa se não encontrar
	var coords *geocoding.Coordinates
	if body.Location != nil && !body.Location.IsEmpty() {
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

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var body UpdateEventRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	startDate, err := parseOptionalTimePtr(body.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	endDate, err := parseOptionalTimePtr(body.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var eventStatus string
	err = db.QueryRowContext(ctx,
		`SELECT status FROM events WHERE id = $1 AND organization_id = $2`,
		eventID, orgID,
	).Scan(&eventStatus)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	categoriesProvided := body.Categories != nil
	var categories []eventservice.TicketCategoryInput
	if categoriesProvided {
		categories = *body.Categories
		if eventStatus != "draft" {
			hasDependencies, err := eventHasTicketDependencies(ctx, db, eventID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao verificar ingressos existentes"})
				return
			}
			if hasDependencies {
				c.JSON(http.StatusUnprocessableEntity, gin.H{
					"error": "não é possível alterar categorias ou lotes de um evento publicado que já possui vendas ou distribuições",
				})
				return
			}
		}
	}

	locationProvided := body.Location != nil
	var locationJSON []byte
	var coords *geocoding.Coordinates
	if locationProvided && !body.Location.IsEmpty() {
		locationJSON = marshalJSON(body.Location)
		coords, _ = geocoding.FromCityState(ctx, body.Location.City, body.Location.State)
	}

	requirementsProvided := body.Requirements != nil
	var requirementsJSON []byte
	if requirementsProvided {
		requirementsJSON = marshalJSON(body.Requirements)
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
                start_date   = CASE WHEN $5::boolean THEN $6::timestamptz ELSE start_date END,
                end_date     = CASE WHEN $7::boolean THEN $8::timestamptz ELSE end_date END,
                location     = CASE WHEN $9::boolean THEN $10::jsonb ELSE location END,
                requirements = CASE WHEN $11::boolean THEN $12::jsonb ELSE requirements END,
                geolocation  = CASE
                                 WHEN $9::boolean THEN
                                   CASE
                                     WHEN $13::float8 IS NOT NULL
                                     THEN ST_SetSRID(ST_MakePoint($14::float8, $13::float8), 4326)
                                     ELSE NULL
                                   END
                                 ELSE geolocation
                               END,
                updated_at   = now()
          WHERE id = $15 AND organization_id = $16`,
		body.Title, body.Description, body.Category, body.Instagram,
		body.StartDate != nil, startDate,
		body.EndDate != nil, endDate,
		locationProvided, dbutil.NullableJSON(locationJSON),
		requirementsProvided, dbutil.NullableJSON(requirementsJSON),
		coordLat(coords), coordLng(coords),
		eventID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar evento: " + err.Error()})
		return
	}

	if categoriesProvided {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM ticket_categories WHERE event_id = $1`, eventID,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover categorias antigas"})
			return
		}
		if len(categories) > 0 {
			if err := eventservice.InsertTicketCategories(ctx, tx, eventID, categories); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao recriar categorias: " + err.Error()})
				return
			}
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

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	snap, err := fetchEventSnapshot(ctx, db, eventID, orgID)
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

	result, err := db.ExecContext(ctx,
		`UPDATE events SET status = 'published', updated_at = now()
          WHERE id = $1 AND organization_id = $2`,
		eventID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao publicar evento"})
		return
	}
	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
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
           LEFT JOIN tickets t ON t.order_id = o.id AND t.status <> 'cancelled'
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("CancelEventHandler: iniciar transação %s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar evento"})
		return
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`UPDATE events SET status = 'cancelled', updated_at = now() WHERE id = $1 AND organization_id = $2`, eventID, orgID,
	)
	if err != nil {
		log.Printf("CancelEventHandler: atualizar evento %s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar evento"})
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		log.Printf("CancelEventHandler: linhas afetadas %s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar evento"})
		return
	}
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// Fecha os lotes de venda e retira os anúncios do Reppy Market do ar.
	if _, err := tx.ExecContext(ctx,
		`UPDATE ticket_batches SET status = 'finished' WHERE event_id = $1 AND status = 'active'`,
		eventID,
	); err != nil {
		log.Printf("CancelEventHandler: encerrar lotes %s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao encerrar lotes"})
		return
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE market_listings SET status = 'canceled', updated_at = now() WHERE event_id = $1 AND status = 'active'`,
		eventID,
	); err != nil {
		log.Printf("CancelEventHandler: encerrar anúncios %s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao encerrar anúncios"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar evento"})
		return
	}

	// Notifica os compradores (pedidos pagos) sobre o cancelamento e reembolso.
	go func() {
		if err := orderservice.SendEventCancelledEmails(db, emailsender.New(""), eventID); err != nil {
			log.Printf("CancelEventHandler: email cancelamento eventID=%s: %v", eventID, err)
		}
	}()

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

// DeleteEventBannerHandler — DELETE /org/:slug/events/:id/banner
func DeleteEventBannerHandler(c *gin.Context) {
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

	result, err := db.ExecContext(ctx,
		`UPDATE events SET image_url = NULL, updated_at = now() WHERE id = $1 AND organization_id = $2`,
		eventID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover banner"})
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover banner"})
		return
	}
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "banner removido"})
}

// ─── Helpers privados ─────────────────────────────────────────────────────────

type eventSnapshot struct {
	Status       string
	Title        *string
	StartDate    *time.Time
	EndDate      *time.Time
	Requirements []byte
}

func fetchEventSnapshot(ctx context.Context, db *sql.DB, eventID, orgID string) (*eventSnapshot, error) {
	var snap eventSnapshot
	err := db.QueryRowContext(ctx,
		`SELECT e.status, e.title, e.start_date, e.end_date, e.requirements
           FROM events e
          WHERE e.id = $1 AND e.organization_id = $2`,
		eventID, orgID,
	).Scan(
		&snap.Status, &snap.Title,
		&snap.StartDate, &snap.EndDate, &snap.Requirements,
	)
	return &snap, err
}

func validateForPublish(snap *eventSnapshot) []string {
	var errs []string

	if snap.Title == nil || strings.TrimSpace(*snap.Title) == "" {
		errs = append(errs, "título obrigatório")
	}

	if snap.StartDate == nil {
		errs = append(errs, "data de início obrigatória")
	} else {
		if snap.StartDate.Before(time.Now().Add(-5 * time.Minute)) {
			errs = append(errs, "data de início não pode estar no passado")
		}
		if snap.EndDate != nil && !snap.EndDate.After(*snap.StartDate) {
			errs = append(errs, "data de término deve ser posterior ao início")
		}
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

func eventHasTicketDependencies(ctx context.Context, db *sql.DB, eventID string) (bool, error) {
	var count int64
	err := db.QueryRowContext(ctx,
		`SELECT
		   (SELECT COUNT(*)
		      FROM tickets t
		      JOIN ticket_batches tb ON tb.id = t.batch_id
		     WHERE tb.event_id = $1)
		 + (SELECT COUNT(*) FROM promoter_distributions WHERE event_id = $1)
		 + (SELECT COUNT(*)
		      FROM coupon_batches cb
		      JOIN coupons c ON c.id = cb.coupon_id
		     WHERE c.event_id = $1)`,
		eventID,
	).Scan(&count)
	return count > 0, err
}

func marshalJSON(value interface{}) []byte {
	b, err := json.Marshal(value)
	if err != nil || string(b) == "null" {
		return nil
	}
	return b
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

func parseOptionalTimePtr(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	return parseOptionalTime(*value)
}

func parseOptionalTime(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
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