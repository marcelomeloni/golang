package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

// GetCheckinDataHandler — GET /org/:slug/events/:id/checkin-data
// Lista todos os ingressos com info do comprador para a tela de check-in.
// Acessível a qualquer membro (checkin_staff inclusive).
func GetCheckinDataHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	_, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// ── Summary ───────────────────────────────────────────────────────────────
	var totalTickets, totalCheckedIn, pendingCheckin int
	var checkinPct float64
	_ = db.QueryRowContext(ctx, `
		SELECT total_tickets, total_checked_in, pending_checkin, checkin_pct
		  FROM v_checkin_summary
		 WHERE event_id = $1`, eventID,
	).Scan(&totalTickets, &totalCheckedIn, &pendingCheckin, &checkinPct)

	// ── Lista de ingressos ────────────────────────────────────────────────────
	rows, err := db.QueryContext(ctx, `
		SELECT
		  t.id,
		  t.qr_code,
		  t.status,
		  t.checked_in_at,
		  tb.name   AS batch_name,
		  tb.type   AS batch_type,
		  u.id      AS user_id,
		  u.full_name,
		  u.email,
		  u.cpf,
		  u.avatar_url,
		  o.id      AS order_id
		FROM tickets t
		JOIN orders o  ON o.id  = t.order_id
		JOIN users  u  ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1 AND o.status = 'paid'
		ORDER BY t.created_at DESC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar ingressos"})
		return
	}
	defer rows.Close()

	tickets := []gin.H{}
	for rows.Next() {
		var (
			ticketID, qrCode, status   string
			batchName, batchType       string
			userID2, orderID           string
			fullName, email            *string
			cpf, avatarURL             *string
			checkedInAt                *string
		)
		if err := rows.Scan(
			&ticketID, &qrCode, &status, &checkedInAt,
			&batchName, &batchType,
			&userID2, &fullName, &email, &cpf, &avatarURL,
			&orderID,
		); err != nil {
			continue
		}
		tickets = append(tickets, gin.H{
			"id":           ticketID,
			"qr_code":      qrCode,
			"status":       status,
			"checked_in_at": checkedInAt,
			"batch_name":   batchName,
			"batch_type":   batchType,
			"order_id":     orderID,
			"user": gin.H{
				"id":         userID2,
				"full_name":  fullName,
				"email":      email,
				"cpf":        cpf,
				"avatar_url": avatarURL,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"total_tickets":    totalTickets,
			"total_checked_in": totalCheckedIn,
			"pending_checkin":  pendingCheckin,
			"checkin_pct":      checkinPct,
		},
		"tickets": tickets,
	})
}

// PatchCheckinHandler — PATCH /org/:slug/events/:id/checkin-data/:ticketID
// Faz ou desfaz o check-in de um ingresso.
func PatchCheckinHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	ticketID := c.Param("ticketID")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	_, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var body struct {
		CheckedIn bool `json:"checked_in"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.CheckedIn {
		_, err = db.ExecContext(ctx, `
			UPDATE tickets SET
			  status        = 'used',
			  checked_in_at = NOW(),
			  checked_in_by = $1
			WHERE id = $2
			  AND order_id IN (SELECT id FROM orders WHERE event_id = $3)`,
			uid, ticketID, eventID,
		)
	} else {
		_, err = db.ExecContext(ctx, `
			UPDATE tickets SET
			  status        = 'valid',
			  checked_in_at = NULL,
			  checked_in_by = NULL
			WHERE id = $1
			  AND order_id IN (SELECT id FROM orders WHERE event_id = $2)`,
			ticketID, eventID,
		)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar check-in"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}