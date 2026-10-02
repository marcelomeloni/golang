package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

func GetParticipantsHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db  := config.GetDB()
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
		  t.id           AS ticket_id,
		  t.qr_code,
		  t.status       AS ticket_status,
		  t.checked_in_at,
		  tb.id          AS batch_id,
		  tb.name        AS batch_name,
		  tb.type        AS batch_type,
		  o.id           AS order_id,
		  to_char(o.created_at, 'DD/MM/YYYY') AS purchase_date,
		  u.id           AS user_id,
		  u.full_name,
		  u.email,
		  u.cpf,
		  u.avatar_url
		FROM tickets t
		JOIN orders        o  ON o.id  = t.order_id
		JOIN users         u  ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1 AND o.status = 'paid'
		ORDER BY o.created_at DESC
	`, eventID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar participantes"})
		return
	}
	defer rows.Close()

	participants := []gin.H{}
	for rows.Next() {
		var (
			ticketID, qrCode, ticketStatus string
			batchID, batchName, batchType  string
			orderID, purchaseDate          string
			userID2                        string
			checkedInAt                    *string
			fullName, email, cpf           *string
			avatarURL                      *string
		)
		if err := rows.Scan(
			&ticketID, &qrCode, &ticketStatus, &checkedInAt,
			&batchID, &batchName, &batchType,
			&orderID, &purchaseDate,
			&userID2, &fullName, &email, &cpf, &avatarURL,
		); err != nil {
			continue
		}
		participants = append(participants, gin.H{
			"ticket_id":     ticketID,
			"qr_code":       qrCode,
			"ticket_status": ticketStatus,
			"checked_in_at": checkedInAt,
			"batch_id":      batchID,
			"batch_name":    batchName,
			"batch_type":    batchType,
			"order_id":      orderID,
			"purchase_date": purchaseDate,
			"user": gin.H{
				"id":         userID2,
				"full_name":  fullName,
				"email":      email,
				"cpf":        cpf,
				"avatar_url": avatarURL,
			},
		})
	}

	c.JSON(http.StatusOK, participants)
}