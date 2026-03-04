package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

// GetParticipantsHandler — GET /org/:slug/events/:id/participants
// Retorna lista completa de participantes (buyer + ticket + batch).
func GetParticipantsHandler(c *gin.Context) {
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
		JOIN orders o         ON o.id  = t.order_id
		JOIN users  u         ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1 AND o.status = 'paid'
		ORDER BY o.created_at DESC`, eventID,
	)
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

// GetComunicadosRecipientsHandler — GET /org/:slug/events/:id/comunicados/recipients
// Retorna lista de destinatários para o composer de comunicados.
// Suporta filtros: ?payment_status=paid&ticket_type=VIP&check_in=true
func GetComunicadosRecipientsHandler(c *gin.Context) {
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

	query := `
		SELECT DISTINCT
		  u.id,
		  u.full_name,
		  u.email,
		  o.status   AS payment_status,
		  tb.name    AS ticket_type,
		  CASE WHEN t.status = 'used' THEN 'Sim' ELSE 'Não' END AS check_in
		FROM tickets t
		JOIN orders o          ON o.id  = t.order_id
		JOIN users  u          ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1`

	args := []interface{}{eventID}
	idx := 2

	if ps := c.Query("payment_status"); ps != "" {
		query += ` AND o.status = $` + itoa(idx)
		args = append(args, ps)
		idx++
	}
	if tt := c.Query("ticket_type"); tt != "" {
		query += ` AND tb.name = $` + itoa(idx)
		args = append(args, tt)
		idx++
	}
	if ci := c.Query("check_in"); ci == "true" {
		query += ` AND t.status = 'used'`
	} else if ci == "false" {
		query += ` AND t.status != 'used'`
	}

	query += ` ORDER BY u.full_name ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar destinatários"})
		return
	}
	defer rows.Close()

	recipients := []gin.H{}
	for rows.Next() {
		var (
			uID, pStatus, ticketType, checkIn string
			fullName, email                   *string
		)
		if err := rows.Scan(&uID, &fullName, &email, &pStatus, &ticketType, &checkIn); err != nil {
			continue
		}
		recipients = append(recipients, gin.H{
			"id":             uID,
			"full_name":      fullName,
			"email":          email,
			"payment_status": pStatus,
			"ticket_type":    ticketType,
			"check_in":       checkIn,
		})
	}

	c.JSON(http.StatusOK, recipients)
}

// itoa converte int para string sem importar strconv em todo arquivo
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "10" // simplificado para até 9 args; expanda se necessário
}