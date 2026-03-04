package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

// GetFinancePainelHandler — GET /org/:slug/events/:id/finance/painel
// Retorna KPIs, vendas por lote e lista completa de pedidos (via v_orders_full).
func GetFinancePainelHandler(c *gin.Context) {
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

	// Garante que o evento pertence a essa org
	var exists bool
	_ = db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)`,
		eventID, orgID,
	).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// ── KPIs ─────────────────────────────────────────────────────────────────
	var grossRevenue, netRevenue, platformFee, refundTotal float64
	var ordersApproved, ordersCancelled int
	_ = db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(total_amount)        FILTER (WHERE status = 'paid'),                0),
		  COALESCE(SUM(net_amount)          FILTER (WHERE status = 'paid'),                0),
		  COALESCE(SUM(platform_fee_amount) FILTER (WHERE status = 'paid'),                0),
		  COALESCE(SUM(total_amount)        FILTER (WHERE status IN ('cancelled','refunded')), 0),
		  COUNT(*)                          FILTER (WHERE status = 'paid'),
		  COUNT(*)                          FILTER (WHERE status IN ('cancelled','refunded'))
		FROM orders
		WHERE event_id = $1`, eventID,
	).Scan(&grossRevenue, &netRevenue, &platformFee, &refundTotal,
		&ordersApproved, &ordersCancelled)

	var avgTicket float64
	if ordersApproved > 0 {
		avgTicket = grossRevenue / float64(ordersApproved)
	}

	// ── Vendas por lote ───────────────────────────────────────────────────────
	batchRows, err := db.QueryContext(ctx, `
		SELECT
		  tb.id,
		  tb.name,
		  tb.price,
		  tb.quantity_total,
		  tb.quantity_sold,
		  COALESCE(SUM(o.net_amount / NULLIF(
		    (SELECT COUNT(*) FROM tickets t2 WHERE t2.order_id = o.id), 0
		  )), 0) AS batch_net_revenue
		FROM ticket_batches tb
		JOIN ticket_categories tc ON tc.id = tb.category_id
		LEFT JOIN tickets t  ON t.batch_id = tb.id
		LEFT JOIN orders  o  ON o.id = t.order_id AND o.status = 'paid'
		WHERE tc.event_id = $1
		GROUP BY tb.id, tb.name, tb.price, tb.quantity_total, tb.quantity_sold
		ORDER BY tb.created_at ASC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar lotes"})
		return
	}
	defer batchRows.Close()

	batches := []gin.H{}
	for batchRows.Next() {
		var bID, bName string
		var bPrice, bNetRevenue float64
		var bQtyTotal, bQtySold int
		if err := batchRows.Scan(&bID, &bName, &bPrice, &bQtyTotal, &bQtySold, &bNetRevenue); err != nil {
			continue
		}
		batches = append(batches, gin.H{
			"id":            bID,
			"name":          bName,
			"price":         bPrice,
			"quantity_total": bQtyTotal,
			"quantity_sold": bQtySold,
			"net_revenue":   bNetRevenue,
		})
	}

	// ── Pedidos completos (v_orders_full) ─────────────────────────────────────
	oRows, err := db.QueryContext(ctx, `
		SELECT
		  id, status, payment_method,
		  total_amount, net_amount, platform_fee_amount, discount_amount,
		  to_char(created_at, 'DD/MM · HH24"h"MI') AS created_at,
		  notes,
		  buyer_name, buyer_email, buyer_cpf,
		  batch_name, batch_type, batch_price
		FROM v_orders_full
		WHERE event_id = $1
		ORDER BY created_at DESC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar pedidos"})
		return
	}
	defer oRows.Close()

	orders := []gin.H{}
	for oRows.Next() {
		var (
			oID, oStatus, oPayment         string
			oTotal, oNet, oFee, oDiscount  float64
			oCreatedAt                     string
			oNotes                         *string
			oBuyerName, oBuyerEmail        *string
			oBuyerCPF                      *string
			oBatchName, oBatchType         *string
			oBatchPrice                    *float64
		)
		if err := oRows.Scan(
			&oID, &oStatus, &oPayment,
			&oTotal, &oNet, &oFee, &oDiscount,
			&oCreatedAt, &oNotes,
			&oBuyerName, &oBuyerEmail, &oBuyerCPF,
			&oBatchName, &oBatchType, &oBatchPrice,
		); err != nil {
			continue
		}
		orders = append(orders, gin.H{
			"id":                  oID,
			"status":              oStatus,
			"payment_method":      oPayment,
			"total_amount":        oTotal,
			"net_amount":          oNet,
			"platform_fee_amount": oFee,
			"discount_amount":     oDiscount,
			"created_at":          oCreatedAt,
			"notes":               oNotes,
			"buyer_name":          oBuyerName,
			"buyer_email":         oBuyerEmail,
			"buyer_cpf":           oBuyerCPF,
			"batch_name":          oBatchName,
			"batch_type":          oBatchType,
			"batch_price":         oBatchPrice,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"kpis": gin.H{
			"gross_revenue":     grossRevenue,
			"net_revenue":       netRevenue,
			"platform_fee":      platformFee,
			"refund_total":      refundTotal,
			"orders_approved":   ordersApproved,
			"orders_cancelled":  ordersCancelled,
			"avg_ticket":        avgTicket,
		},
		"batches": batches,
		"orders":  orders,
	})
}

// GetFinanceResumoHandler — GET /org/:slug/events/:id/finance/resumo
// Retorna totais consolidados + performance por lote.
func GetFinanceResumoHandler(c *gin.Context) {
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

	var exists bool
	_ = db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)`,
		eventID, orgID,
	).Scan(&exists)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// ── Totais ────────────────────────────────────────────────────────────────
	var grossRevenue, netRevenue, platformFee, discountTotal, refundTotal float64
	_ = db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(total_amount)        FILTER (WHERE status = 'paid'), 0),
		  COALESCE(SUM(net_amount)          FILTER (WHERE status = 'paid'), 0),
		  COALESCE(SUM(platform_fee_amount) FILTER (WHERE status = 'paid'), 0),
		  COALESCE(SUM(discount_amount)     FILTER (WHERE status = 'paid'), 0),
		  COALESCE(SUM(total_amount)        FILTER (WHERE status IN ('cancelled','refunded')), 0)
		FROM orders WHERE event_id = $1`, eventID,
	).Scan(&grossRevenue, &netRevenue, &platformFee, &discountTotal, &refundTotal)

	// ── Performance por lote ──────────────────────────────────────────────────
	rows, err := db.QueryContext(ctx, `
		SELECT
		  tb.id,
		  tb.name,
		  tb.price,
		  tb.quantity_total,
		  tb.quantity_sold,
		  COALESCE((
		    SELECT SUM(o2.net_amount)
		      FROM tickets t2
		      JOIN orders o2 ON o2.id = t2.order_id
		     WHERE t2.batch_id = tb.id AND o2.status = 'paid'
		  ), 0) AS net_revenue
		FROM ticket_batches tb
		JOIN ticket_categories tc ON tc.id = tb.category_id
		WHERE tc.event_id = $1
		ORDER BY tc.position ASC, tb.created_at ASC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar lotes"})
		return
	}
	defer rows.Close()

	batches := []gin.H{}
	for rows.Next() {
		var bID, bName string
		var bPrice, bNetRevenue float64
		var bQtyTotal, bQtySold int
		if err := rows.Scan(&bID, &bName, &bPrice, &bQtyTotal, &bQtySold, &bNetRevenue); err != nil {
			continue
		}
		batches = append(batches, gin.H{
			"id":             bID,
			"name":           bName,
			"price":          bPrice,
			"quantity_total": bQtyTotal,
			"quantity_sold":  bQtySold,
			"net_revenue":    bNetRevenue,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"totals": gin.H{
			"gross_revenue":  grossRevenue,
			"net_revenue":    netRevenue,
			"platform_fee":   platformFee,
			"discount_total": discountTotal,
			"refund_total":   refundTotal,
		},
		"batches": batches,
	})
}