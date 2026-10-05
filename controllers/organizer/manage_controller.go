package organizer

import (
	"database/sql"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/internal/dbutil"
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

// GetEventBatchesHandler — GET /org/:slug/events/:id/batches
// Retorna lotes (batches) ativos do evento com detalhes completos.
// Acessível a promoter, admin, owner.
func GetEventBatchesHandler(c *gin.Context) {
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

	// Verificar se o usuário tem role promoter ou superior
	var role string
	err = db.QueryRowContext(ctx, `
		SELECT role FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, uid).Scan(&role)
	if err != nil || (role != "owner" && role != "admin" && role != "promoter") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente"})
		return
	}

	// Verificar se evento pertence à org
	var exists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)
	`, eventID, orgID).Scan(&exists)
	if err != nil || !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// Buscar categorias com lotes ativos
	rows, err := db.QueryContext(ctx, `
		SELECT
			tc.id as category_id,
			tc.name as category_name,
			tc.type as category_type,
			tb.id as batch_id,
			tb.name as batch_name,
			tb.price,
			tb.quantity_total,
			tb.quantity_sold,
			tb.max_purchase,
			tb.status,
			to_char(tb.start_date, 'YYYY-MM-DD"T"HH24:MI:SS') as start_date,
			to_char(tb.end_date, 'YYYY-MM-DD"T"HH24:MI:SS') as end_date
		FROM ticket_categories tc
		LEFT JOIN ticket_batches tb ON tb.category_id = tc.id
			AND tb.status = 'active'
			AND (tb.start_date IS NULL OR tb.start_date <= NOW())
			AND (tb.end_date IS NULL OR tb.end_date > NOW())
		WHERE tc.event_id = $1
		ORDER BY tc.position ASC, tb.created_at ASC
	`, eventID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar lotes"})
		return
	}
	defer rows.Close()

	type Batch struct {
		ID            string  `json:"id"`
		Name          string  `json:"name"`
		Price         float64 `json:"price"`
		QuantityTotal int     `json:"quantity_total"`
		QuantitySold  int     `json:"quantity_sold"`
		MaxPurchase   int     `json:"max_purchase"`
		Status        string  `json:"status"`
		StartDate     *string `json:"start_date,omitempty"`
		EndDate       *string `json:"end_date,omitempty"`
		Available     int     `json:"available"`
	}

	type Category struct {
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Type    string  `json:"type"`
		Batches []Batch `json:"batches"`
	}

	catsMap := make(map[string]*Category)
	catsOrder := []string{}

	for rows.Next() {
		var (
			catID, catName, catType         string
			batchID, batchName, batchStatus sql.NullString
			price                           sql.NullFloat64
			qtyTotal, qtySold, maxP         sql.NullInt64
			startDate, endDate              sql.NullString
		)
		if err := rows.Scan(
			&catID, &catName, &catType,
			&batchID, &batchName, &price,
			&qtyTotal, &qtySold, &maxP, &batchStatus,
			&startDate, &endDate,
		); err != nil {
			continue
		}

		if _, seen := catsMap[catID]; !seen {
			catsMap[catID] = &Category{
				ID:   catID,
				Name: catName,
				Type: catType,
			}
			catsOrder = append(catsOrder, catID)
		}

		if batchID.Valid {
			total := int(qtyTotal.Int64)
			sold := int(qtySold.Int64)
			batch := Batch{
				ID:            batchID.String,
				Name:          batchName.String,
				Price:         price.Float64,
				QuantityTotal: total,
				QuantitySold:  sold,
				MaxPurchase:   int(maxP.Int64),
				Status:        batchStatus.String,
				StartDate:     dbutil.StrPtr(startDate),
				EndDate:       dbutil.StrPtr(endDate),
				Available:     total - sold,
			}
			catsMap[catID].Batches = append(catsMap[catID].Batches, batch)
		}
	}

	// Filtrar apenas categorias que têm lotes ativos disponíveis
	categoriesOut := make([]Category, 0, len(catsOrder))
	for _, catID := range catsOrder {
		cat := catsMap[catID]
		// Filtrar lotes ativos com disponibilidade > 0
		activeBatches := make([]Batch, 0, len(cat.Batches))
		for _, b := range cat.Batches {
			if b.Status == "active" && b.Available > 0 {
				activeBatches = append(activeBatches, b)
			}
		}
		if len(activeBatches) > 0 {
			cat.Batches = activeBatches
			categoriesOut = append(categoriesOut, *cat)
		}
	}

	c.JSON(http.StatusOK, gin.H{"categories": categoriesOut})
}