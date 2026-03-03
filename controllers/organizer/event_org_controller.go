package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// GET /org/:slug/events
// Lista todos os eventos da org com stats básicas
func GetOrgEventsHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// Filtros opcionais via query params
	statusFilter := c.Query("status") // published | draft | cancelled | finished

	query := `
		SELECT e.id, e.title, e.slug, e.status, e.image_url,
		       to_char(e.start_date, 'DD Mon, HH24:MI') AS start_date,
		       e.location->>'venue'   AS venue,
		       e.location->>'city'    AS city,
		       COUNT(t.id)            AS tickets_sold,
		       COALESCE(
		         (SELECT SUM(tb2.quantity_total)
		            FROM ticket_batches tb2
		           WHERE tb2.event_id = e.id), 0
		       )                      AS total_capacity
		  FROM events e
		  LEFT JOIN orders o  ON o.event_id = e.id AND o.status = 'paid'
		  LEFT JOIN tickets t ON t.order_id = o.id
		 WHERE e.organization_id = $1`

	args := []interface{}{orgID}

	if statusFilter != "" {
		query += ` AND e.status = $2`
		args = append(args, statusFilter)
	}

	query += ` GROUP BY e.id, e.title, e.slug, e.status, e.image_url, e.start_date, e.location
	           ORDER BY e.start_date DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar eventos"})
		return
	}
	defer rows.Close()

	type EventRow struct {
		ID            string
		Title         string
		Slug          string
		Status        string
		ImageURL      *string
		StartDate     *string
		Venue         *string
		City          *string
		TicketsSold   int
		TotalCapacity int
	}

	var events []gin.H
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.ID, &e.Title, &e.Slug, &e.Status, &e.ImageURL,
			&e.StartDate, &e.Venue, &e.City, &e.TicketsSold, &e.TotalCapacity); err != nil {
			continue
		}
		events = append(events, gin.H{
			"id":             e.ID,
			"title":          e.Title,
			"slug":           e.Slug,
			"status":         e.Status,
			"image_url":      e.ImageURL,
			"start_date":     e.StartDate,
			"venue":          e.Venue,
			"city":           e.City,
			"tickets_sold":   e.TicketsSold,
			"total_capacity": e.TotalCapacity,
		})
	}

	if events == nil {
		events = []gin.H{}
	}

	c.JSON(http.StatusOK, events)
}

// GET /org/:slug/events/:eventID
// Retorna detalhes completos de um evento específico
func GetOrgEventDetailHandler(c *gin.Context) {
	slug := c.Param("slug")
	eventID := c.Param("eventID")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// ── Dados do evento ───────────────────────────────────────────────────────
	type EventDetail struct {
		ID          string
		Title       string
		Slug        string
		Description *string
		Status      string
		ImageURL    *string
		LogoURL     *string
		StartDate   *string
		EndDate     *string
		Location    *string
		FormFields  *string
		Views       int
		CreatedAt   string
	}

	var e EventDetail
	err = db.QueryRowContext(ctx,
		`SELECT id, title, slug, description, status, image_url, logo_url,
		        to_char(start_date, 'YYYY-MM-DD"T"HH24:MI:SS') AS start_date,
		        to_char(end_date,   'YYYY-MM-DD"T"HH24:MI:SS') AS end_date,
		        location::text, form_fields::text, views,
		        to_char(created_at, 'YYYY-MM-DD') AS created_at
		   FROM events
		  WHERE id = $1 AND organization_id = $2`, eventID, orgID,
	).Scan(&e.ID, &e.Title, &e.Slug, &e.Description, &e.Status,
		&e.ImageURL, &e.LogoURL, &e.StartDate, &e.EndDate,
		&e.Location, &e.FormFields, &e.Views, &e.CreatedAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// ── Lotes do evento ───────────────────────────────────────────────────────
	batchRows, err := db.QueryContext(ctx,
		`SELECT id, name, type, price, quantity_total, quantity_sold,
		        status, fee_payer, availability, min_purchase, max_purchase,
		        to_char(start_date, 'YYYY-MM-DD"T"HH24:MI:SS') AS start_date,
		        to_char(end_date,   'YYYY-MM-DD"T"HH24:MI:SS') AS end_date
		   FROM ticket_batches
		  WHERE event_id = $1
		  ORDER BY created_at ASC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar lotes"})
		return
	}
	defer batchRows.Close()

	type BatchRow struct {
		ID            string
		Name          string
		Type          string
		Price         float64
		QuantityTotal int
		QuantitySold  int
		Status        string
		FeePayer      string
		Availability  string
		MinPurchase   int
		MaxPurchase   int
		StartDate     *string
		EndDate       *string
	}

	var batches []gin.H
	for batchRows.Next() {
		var b BatchRow
		if err := batchRows.Scan(&b.ID, &b.Name, &b.Type, &b.Price,
			&b.QuantityTotal, &b.QuantitySold, &b.Status, &b.FeePayer,
			&b.Availability, &b.MinPurchase, &b.MaxPurchase,
			&b.StartDate, &b.EndDate); err != nil {
			continue
		}
		batches = append(batches, gin.H{
			"id":             b.ID,
			"name":           b.Name,
			"type":           b.Type,
			"price":          b.Price,
			"quantity_total": b.QuantityTotal,
			"quantity_sold":  b.QuantitySold,
			"status":         b.Status,
			"fee_payer":      b.FeePayer,
			"availability":   b.Availability,
			"min_purchase":   b.MinPurchase,
			"max_purchase":   b.MaxPurchase,
			"start_date":     b.StartDate,
			"end_date":       b.EndDate,
		})
	}

	if batches == nil {
		batches = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":          e.ID,
		"title":       e.Title,
		"slug":        e.Slug,
		"description": e.Description,
		"status":      e.Status,
		"image_url":   e.ImageURL,
		"logo_url":    e.LogoURL,
		"start_date":  e.StartDate,
		"end_date":    e.EndDate,
		"location":    e.Location,
		"form_fields": e.FormFields,
		"views":       e.Views,
		"created_at":  e.CreatedAt,
		"batches":     batches,
	})
}