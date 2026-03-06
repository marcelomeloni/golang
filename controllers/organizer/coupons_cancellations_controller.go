package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)


func GetCouponsHandler(c *gin.Context) {
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

	rows, err := db.QueryContext(ctx, `
		SELECT
		  c.id,
		  c.code,
		  c.discount_type,
		  c.discount_value,
		  c.max_uses,
		  c.used_count,
		  to_char(c.expires_at, 'YYYY-MM-DD"T"HH24:MI:SS'),
		  c.active,
		  to_char(c.created_at, 'YYYY-MM-DD'),
		  -- agrega os batch_ids vinculados em array (NULL se não houver)
		  COALESCE(
		    ARRAY_AGG(cb.batch_id::text) FILTER (WHERE cb.batch_id IS NOT NULL),
		    ARRAY[]::text[]
		  ) AS batch_ids
		FROM coupons c
		LEFT JOIN coupon_batches cb ON cb.coupon_id = c.id
		WHERE c.event_id = $1
		GROUP BY c.id
		ORDER BY c.created_at DESC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar cupons"})
		return
	}
	defer rows.Close()

	coupons := []gin.H{}
	for rows.Next() {
		var (
			cID, cCode, cDiscType string
			cDiscValue            float64
			cMaxUses, cUsedCount  *int
			cExpiresAt            *string
			cActive               bool
			cCreatedAt            string
			cBatchIDs             []string
		)
		if err := rows.Scan(
			&cID, &cCode, &cDiscType, &cDiscValue,
			&cMaxUses, &cUsedCount, &cExpiresAt,
			&cActive, &cCreatedAt,
			pq.Array(&cBatchIDs),
		); err != nil {
			continue
		}
		coupons = append(coupons, gin.H{
			"id":             cID,
			"code":           cCode,
			"discount_type":  cDiscType,
			"discount_value": cDiscValue,
			"max_uses":       cMaxUses,
			"used_count":     cUsedCount,
			"expires_at":     cExpiresAt,
			"active":         cActive,
			"created_at":     cCreatedAt,
			"batch_ids":      cBatchIDs, // [] = todos os lotes, [id1, id2] = específicos
		})
	}

	c.JSON(http.StatusOK, coupons)
}


// CreateCouponHandler — POST /org/:slug/events/:id/coupons
func CreateCouponHandler(c *gin.Context) {
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

	var body struct {
		Code          string   `json:"code"           binding:"required"`
		DiscountType  string   `json:"discount_type"  binding:"required"`
		DiscountValue float64  `json:"discount_value" binding:"required"`
		MaxUses       *int     `json:"max_uses"`
		ExpiresAt     *string  `json:"expires_at"`
		BatchIDs      []string `json:"batch_ids"` // vazio = aplica a todos
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.DiscountType != "percentage" && body.DiscountType != "fixed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "discount_type inválido"})
		return
	}

	// Valida que os batch_ids informados pertencem ao evento
	if len(body.BatchIDs) > 0 {
		var validCount int
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			  FROM ticket_batches tb
			  JOIN ticket_categories tc ON tc.id = tb.category_id
			 WHERE tc.event_id = $1
			   AND tb.id = ANY($2::uuid[])`,
			eventID, pq.Array(body.BatchIDs),
		).Scan(&validCount)
		if err != nil || validCount != len(body.BatchIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "um ou mais lotes inválidos para este evento"})
			return
		}
	}

	// ── Tudo em transação ────────────────────────────────────────────────────
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	// 1. Insere o cupom
	var newID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO coupons
		  (event_id, code, discount_type, discount_value, max_uses, expires_at, active)
		VALUES ($1, UPPER($2), $3, $4, $5, $6::timestamptz, true)
		RETURNING id`,
		eventID, body.Code, body.DiscountType, body.DiscountValue,
		body.MaxUses, body.ExpiresAt,
	).Scan(&newID)
	if err != nil {
		// Código duplicado → unique constraint
		c.JSON(http.StatusConflict, gin.H{"error": "já existe um cupom com esse código neste evento"})
		return
	}

	// 2. Insere os vínculos com lotes (se informados)
	if len(body.BatchIDs) > 0 {
		for _, batchID := range body.BatchIDs {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO coupon_batches (coupon_id, batch_id)
				VALUES ($1, $2)
				ON CONFLICT (coupon_id, batch_id) DO NOTHING`,
				newID, batchID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao vincular lote ao cupom"})
				return
			}
		}
	}

	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":        newID,
		"batch_ids": body.BatchIDs,
	})
}

// PatchCouponHandler — PATCH /org/:slug/events/:id/coupons/:couponID
func PatchCouponHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	couponID := c.Param("couponID")
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

	var body struct {
		Active *bool `json:"active"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Active == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "campo 'active' obrigatório"})
		return
	}

	_, err = db.ExecContext(ctx,
		`UPDATE coupons SET active = $1 WHERE id = $2 AND event_id = $3`,
		*body.Active, couponID, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar cupom"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteCouponHandler — DELETE /org/:slug/events/:id/coupons/:couponID
func DeleteCouponHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	couponID := c.Param("couponID")
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

	_, err = db.ExecContext(ctx,
		`DELETE FROM coupons WHERE id = $1 AND event_id = $2`,
		couponID, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao deletar cupom"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ─── CANCELAMENTOS ───────────────────────────────────────────────────────────

// GetCancellationsHandler — GET /org/:slug/events/:id/cancellations
// Retorna orders canceladas/reembolsadas + refunds associados.
func GetCancellationsHandler(c *gin.Context) {
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

	rows, err := db.QueryContext(ctx, `
		SELECT
		  r.id,
		  r.order_id,
		  o.total_amount,
		  r.amount,
		  r.reason,
		  r.status,
		  to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SS'),
		  u.full_name   AS buyer_name,
		  tb.name       AS batch_name
		FROM refunds r
		JOIN orders  o  ON o.id  = r.order_id
		JOIN users   u  ON u.id  = o.user_id
		JOIN tickets t  ON t.order_id = o.id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1
		GROUP BY r.id, r.order_id, o.total_amount, r.amount, r.reason,
		         r.status, r.created_at, u.full_name, tb.name
		ORDER BY r.created_at DESC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar cancelamentos"})
		return
	}
	defer rows.Close()

	refunds := []gin.H{}
	for rows.Next() {
		var (
			rID, rOrderID, rStatus, rCreatedAt string
			rReason, rBuyerName, rBatchName    *string
			rAmount, oTotal                    float64
		)
		if err := rows.Scan(
			&rID, &rOrderID, &oTotal, &rAmount, &rReason,
			&rStatus, &rCreatedAt, &rBuyerName, &rBatchName,
		); err != nil {
			continue
		}
		refunds = append(refunds, gin.H{
			"id":           rID,
			"order_id":     rOrderID,
			"order_total":  oTotal,
			"amount":       rAmount,
			"reason":       rReason,
			"status":       rStatus,
			"created_at":   rCreatedAt,
			"buyer_name":   rBuyerName,
			"batch_name":   rBatchName,
		})
	}

	c.JSON(http.StatusOK, refunds)
}

// PatchRefundHandler — PATCH /org/:slug/events/:id/cancellations/:refundID
// Aprova ou rejeita um pedido de reembolso.
func PatchRefundHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	refundID := c.Param("refundID")
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

	var body struct {
		Status string `json:"status" binding:"required"` // "approved" | "rejected"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Status != "approved" && body.Status != "rejected" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status inválido"})
		return
	}

	newStatus := body.Status
	if body.Status == "approved" {
		newStatus = "completed"
	}

	_, err = db.ExecContext(ctx,
		`UPDATE refunds SET status = $1
		  WHERE id = $2
		    AND order_id IN (SELECT id FROM orders WHERE event_id = $3)
		    AND status = 'pending'`,
		newStatus, refundID, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar reembolso"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}