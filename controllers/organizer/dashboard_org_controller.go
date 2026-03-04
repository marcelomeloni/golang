package organizer

import (
	"database/sql"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

type EventSummary struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Slug          string  `json:"slug"`
	Status        string  `json:"status"`
	ImageURL      *string `json:"image_url"`
	StartDate     *string `json:"start_date"`
	Venue         *string `json:"venue"`
	TicketsSold   int     `json:"tickets_sold"`
	TotalCapacity int     `json:"total_capacity"`
}

type OrgOverviewResponse struct {
	TotalTickets   int            `json:"total_tickets"`
	TotalRevenue   float64        `json:"total_revenue"`
	TotalMembers   int            `json:"total_members"`
	UpcomingEvents []EventSummary `json:"upcoming_events"`
}

func GetOrgOverviewHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id FROM organizations o
		 JOIN organization_members om ON om.organization_id = o.id
		WHERE o.slug = $1 AND om.user_id = $2`, 
		slug, userID.(string),
	).Scan(&orgID)

	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao validar acesso"})
		return
	}

	var res OrgOverviewResponse
	res.UpcomingEvents = make([]EventSummary, 0)

	err = db.QueryRowContext(ctx,
		`SELECT COUNT(t.id), COALESCE(SUM(o.net_amount), 0)
		   FROM orders o
		   JOIN tickets t ON t.order_id = o.id
		  WHERE o.event_id IN (SELECT id FROM events WHERE organization_id = $1) 
		    AND o.status = 'paid'`, 
		orgID,
	).Scan(&res.TotalTickets, &res.TotalRevenue)
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao calcular estatísticas de vendas"})
		return
	}

	err = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM organization_members WHERE organization_id = $1`, orgID,
	).Scan(&res.TotalMembers)
	if err != nil && err != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar membros"})
		return
	}

	rows, err := db.QueryContext(ctx,
		`SELECT e.id, e.title, e.slug, e.status, e.image_url,
				to_char(e.start_date, 'DD Mon, HH24:MI') AS start_date,
				e.location->>'venue' AS venue,
				COUNT(t.id) AS tickets_sold,
				COALESCE((SELECT SUM(tb2.quantity_total) FROM ticket_batches tb2 WHERE tb2.event_id = e.id), 0) AS total_capacity
		   FROM events e
		   LEFT JOIN orders o  ON o.event_id = e.id AND o.status = 'paid'
		   LEFT JOIN tickets t ON t.order_id = o.id
		  WHERE e.organization_id = $1 AND e.status IN ('published', 'draft')
		  GROUP BY e.id, e.title, e.slug, e.status, e.image_url, e.start_date, e.location
		  ORDER BY e.start_date ASC
		  LIMIT 5`, 
		orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar eventos"})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var e EventSummary
		if err := rows.Scan(&e.ID, &e.Title, &e.Slug, &e.Status, &e.ImageURL, &e.StartDate, &e.Venue, &e.TicketsSold, &e.TotalCapacity); err != nil {
			continue
		}
		res.UpcomingEvents = append(res.UpcomingEvents, e)
	}

	c.JSON(http.StatusOK, res)
}