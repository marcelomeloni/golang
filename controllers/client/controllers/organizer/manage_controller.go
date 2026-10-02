package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

func GetEventManageHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// ── Dados principais do evento ────────────────────────────────────────────
	var (
		id, title, slug, status, createdAt string
		description, category, instagram   *string
		imageURL, logoURL                  *string
		startDate, endDate                 *string
		location, requirements             *string
		views                              int
	)
	err = db.QueryRowContext(ctx, `
		SELECT id, title, slug, description, category, instagram, status,
		       image_url, logo_url,
		       to_char(start_date, 'YYYY-MM-DD"T"HH24:MI:SS'),
		       to_char(end_date,   'YYYY-MM-DD"T"HH24:MI:SS'),
		       location::text, requirements::text, views,
		       to_char(created_at, 'YYYY-MM-DD')
		  FROM events
		 WHERE id = $1 AND organization_id = $2`, eventID, orgID,
	).Scan(
		&id, &title, &slug, &description, &category, &instagram, &status,
		&imageURL, &logoURL, &startDate, &endDate,
		&location, &requirements, &views, &createdAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// ── Email da organização ──────────────────────────────────────────────────
	var orgEmail *string
	_ = db.QueryRowContext(ctx, `
		SELECT email FROM organizations WHERE id = $1`, orgID,
	).Scan(&orgEmail)

	// ── Categorias de ingresso ────────────────────────────────────────────────
	catRows, catErr := db.QueryContext(ctx, `
		SELECT id, name
		  FROM ticket_categories
		 WHERE event_id = $1
		 ORDER BY position ASC, created_at ASC`, eventID,
	)
	categories := []gin.H{}
	if catErr == nil {
		defer catRows.Close()
		for catRows.Next() {
			var cID, cName string
			if err := catRows.Scan(&cID, &cName); err != nil {
				continue
			}
			categories = append(categories, gin.H{
				"id":   cID,
				"name": cName,
			})
		}
	}

	// ── Check-in summary ──────────────────────────────────────────────────────
	var (
		totalTickets, totalCheckedIn, pendingCheckin int
		checkinPct                                   float64
	)
	_ = db.QueryRowContext(ctx, `
		SELECT total_tickets, total_checked_in, pending_checkin, checkin_pct
		  FROM v_checkin_summary
		 WHERE event_id = $1`, eventID,
	).Scan(&totalTickets, &totalCheckedIn, &pendingCheckin, &checkinPct)

	// ── Totais financeiros ────────────────────────────────────────────────────
	var (
		grossRevenue, netRevenue, platformFee, discountTotal float64
		ticketsSold, ordersApproved, ordersCancelled         int
	)
	_ = db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(total_amount)        FILTER (WHERE status = 'paid'),                  0),
		  COALESCE(SUM(net_amount)          FILTER (WHERE status = 'paid'),                  0),
		  COALESCE(SUM(platform_fee_amount) FILTER (WHERE status = 'paid'),                  0),
		  COALESCE(SUM(discount_amount)     FILTER (WHERE status = 'paid'),                  0),
		  COUNT(*)                          FILTER (WHERE status = 'paid'),
		  COUNT(*)                          FILTER (WHERE status IN ('cancelled','refunded'))
		FROM orders
		WHERE event_id = $1`, eventID,
	).Scan(&grossRevenue, &netRevenue, &platformFee, &discountTotal,
		&ordersApproved, &ordersCancelled)

	_ = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM tickets t
		  JOIN orders o ON o.id = t.order_id
		 WHERE o.event_id = $1 AND o.status = 'paid'`, eventID,
	).Scan(&ticketsSold)

	// ── Capacidade total ──────────────────────────────────────────────────────
	var totalCapacity int
	_ = db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tb.quantity_total), 0)
		  FROM ticket_batches tb
		  JOIN ticket_categories tc ON tc.id = tb.category_id
		 WHERE tc.event_id = $1`, eventID,
	).Scan(&totalCapacity)

	// ── Conta bancária cadastrada? ────────────────────────────────────────────
	var hasBankAccount bool
	_ = db.QueryRowContext(ctx, `
		SELECT EXISTS(
		  SELECT 1 FROM organization_bank_accounts
		   WHERE organization_id = $1
		)`, orgID,
	).Scan(&hasBankAccount)

	c.JSON(http.StatusOK, gin.H{
		"event": gin.H{
			"id":                id,
			"title":             title,
			"slug":              slug,
			"description":       description,
			"category":          category,
			"instagram":         instagram,
			"status":            status,
			"image_url":         imageURL,
			"logo_url":          logoURL,
			"start_date":        startDate,
			"end_date":          endDate,
			"location":          location,
			"requirements":      requirements,
			"views":             views,
			"created_at":        createdAt,
			"org_email":         orgEmail,
			"ticket_categories": categories,
		},
		"stats": gin.H{
			"tickets_sold":     ticketsSold,
			"total_capacity":   totalCapacity,
			"gross_revenue":    grossRevenue,
			"net_revenue":      netRevenue,
			"platform_fee":     platformFee,
			"discount_total":   discountTotal,
			"orders_approved":  ordersApproved,
			"orders_cancelled": ordersCancelled,
		},
		"checkin": gin.H{
			"total_tickets":    totalTickets,
			"total_checked_in": totalCheckedIn,
			"pending_checkin":  pendingCheckin,
			"checkin_pct":      checkinPct,
		},
		"has_bank_account": hasBankAccount,
	})
}