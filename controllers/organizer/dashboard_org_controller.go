package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// GET /org/:slug/overview
// Retorna stats gerais da org: total de ingressos vendidos, receita, membros e próximos eventos
func GetOrgOverviewHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	// Verifica membership e pega org_id
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

	// ── 1. Total de ingressos vendidos e receita líquida ─────────────────────
	var totalTickets int
	var totalRevenue float64
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(t.id), COALESCE(SUM(o.net_amount), 0)
		   FROM orders o
		   JOIN tickets t ON t.order_id = o.id
		  WHERE o.event_id IN (
		    SELECT id FROM events WHERE organization_id = $1
		  ) AND o.status = 'paid'`, orgID,
	).Scan(&totalTickets, &totalRevenue)

	// ── 2. Total de membros ───────────────────────────────────────────────────
	var totalMembers int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM organization_members WHERE organization_id = $1`, orgID,
	).Scan(&totalMembers)

	// ── 3. Próximos eventos (máx 5) ───────────────────────────────────────────
	rows, err := db.QueryContext(ctx,
		`SELECT e.id, e.title, e.slug, e.status, e.image_url,
		        to_char(e.start_date, 'DD Mon, HH24:MI') AS start_date,
		        e.location->>'venue' AS venue,
		        COUNT(t.id) AS tickets_sold,
		        COALESCE(
		          (SELECT SUM(tb2.quantity_total)
		             FROM ticket_batches tb2
		            WHERE tb2.event_id = e.id), 0
		        ) AS total_capacity
		   FROM events e
		   LEFT JOIN orders o  ON o.event_id = e.id AND o.status = 'paid'
		   LEFT JOIN tickets t ON t.order_id = o.id
		  WHERE e.organization_id = $1
		    AND e.status IN ('published', 'draft')
		  GROUP BY e.id, e.title, e.slug, e.status, e.image_url, e.start_date, e.location
		  ORDER BY e.start_date ASC
		  LIMIT 5`, orgID,
	)
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
		TicketsSold   int
		TotalCapacity int
	}

	var events []gin.H
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.ID, &e.Title, &e.Slug, &e.Status, &e.ImageURL,
			&e.StartDate, &e.Venue, &e.TicketsSold, &e.TotalCapacity); err != nil {
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
			"tickets_sold":   e.TicketsSold,
			"total_capacity": e.TotalCapacity,
		})
	}

	if events == nil {
		events = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_tickets":  totalTickets,
		"total_revenue":  totalRevenue,
		"total_members":  totalMembers,
		"upcoming_events": events,
	})
}