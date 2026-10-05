package organizer

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// resolveCheckinOrg valida que o usuário é membro da org e que pode operar o
// balcão de check-in, devolvendo o orgID.
//
// Promoter fica de fora de propósito: a UI mantém promoter em modo somente
// leitura, então a API precisa do mesmo filtro para não ser contornada chamando
// o endpoint direto. owner/admin podem tudo; checkin_staff é o papel do balcão.
func resolveCheckinOrg(ctx context.Context, db *sql.DB, slug, uid string) (string, bool) {
	var orgID, role string
	// Uma query só: o balcão de check-in sofre com queries extras por leitura.
	if err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role
		   FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`,
		slug, uid,
	).Scan(&orgID, &role); err != nil {
		return "", false
	}

	switch role {
	case "owner", "admin", "checkin_staff":
		return orgID, true
	default:
		return "", false
	}
}

// nonDigitsRe casa qualquer coisa que não seja dígito.
var nonDigitsRe = regexp.MustCompile(`\D`)

// Status possíveis de um ingresso.
const (
	ticketStatusValid     = "valid"
	ticketStatusUsed      = "used"
	ticketStatusCancelled = "cancelled"
)

// GetCheckinDataHandler — GET /org/:slug/events/:id/checkin-data
// Lista todos os ingressos com info do comprador para a tela de check-in.
// Acessível a qualquer membro (checkin_staff inclusive).
func GetCheckinDataHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid, _ := userID.(string)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, ok := resolveCheckinOrg(ctx, db, orgSlug, uid)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// Sem isso, um membro da org A leria os ingressos de um evento da org B
	// passando o id do evento na URL.
	if !eventBelongsToOrg(ctx, db, eventID, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
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
		WHERE o.event_id = $1
		  AND o.status   = 'paid'
		  AND t.status IN ('valid', 'used')
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

// checkinTicket é o ingresso localizado pelo token do QR code.
type checkinTicket struct {
	ID          string
	QRCode      string
	Status      string
	CheckedInAt sql.NullTime
	CheckedInBy sql.NullString
	BatchName   sql.NullString
	BatchType   sql.NullString
	OrderID     string
	UserID      string
	FullName    sql.NullString
	Email       sql.NullString
	CPF         sql.NullString
	AvatarURL   sql.NullString
}

func (t checkinTicket) payload() gin.H {
	var checkedInAt any
	if t.CheckedInAt.Valid {
		checkedInAt = t.CheckedInAt.Time.Format(time.RFC3339)
	}

	var checkedInBy any
	if t.CheckedInBy.Valid {
		checkedInBy = t.CheckedInBy.String
	}

	return gin.H{
		"id":            t.ID,
		"qr_code":       t.QRCode,
		"status":        t.Status,
		"checked_in_at": checkedInAt,
		"checked_in_by": checkedInBy,
		"batch_name":    t.BatchName.String,
		"batch_type":    t.BatchType.String,
		"order_id":      t.OrderID,
		"user": gin.H{
			"id":         t.UserID,
			"full_name":  t.FullName.String,
			"email":      t.Email.String,
			"cpf":        t.CPF.String,
			"avatar_url": t.AvatarURL.String,
		},
	}
}

// validTicketQRCode confere o formato gerado por orderservice.GenerateQRCode
// ("RPY-" + 16 hex). Rejeita lixo antes de gastar uma consulta no banco.
func validTicketQRCode(qr string) bool {
	if len(qr) != len("RPY-")+16 {
		return false
	}
	if !strings.HasPrefix(qr, "RPY-") {
		return false
	}
	for _, ch := range qr[len("RPY-"):] {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			return false
		}
	}
	return true
}

// loadTicketByQRCode localiza o ingresso pelo token impresso no QR/PDF oficial,
// sempre restrito ao evento da URL e a pedidos pagos.
func loadTicketByQRCode(ctx context.Context, db *sql.DB, qrCode, eventID string) (*checkinTicket, error) {
	var t checkinTicket
	err := db.QueryRowContext(ctx, `
		SELECT
		  t.id, t.qr_code, t.status, t.checked_in_at, t.checked_in_by,
		  tb.name, tb.type,
		  o.id  AS order_id,
		  u.id  AS user_id,
		  u.full_name, u.email, u.cpf, u.avatar_url
		FROM tickets t
		JOIN orders o          ON o.id  = t.order_id
		JOIN users  u          ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.qr_code = $1
		  AND o.event_id = $2
		  AND o.status   = 'paid'
		LIMIT 1`, qrCode, eventID,
	).Scan(
		&t.ID, &t.QRCode, &t.Status, &t.CheckedInAt, &t.CheckedInBy,
		&t.BatchName, &t.BatchType,
		&t.OrderID,
		&t.UserID,
		&t.FullName, &t.Email, &t.CPF, &t.AvatarURL,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// cleanCPFDigits devolve só os dígitos do CPF, para comparar com a coluna
// gerada cpf_digits.
func cleanCPFDigits(raw string) string {
	return nonDigitsRe.ReplaceAllString(raw, "")
}

// formatCPFDigits formata 11 dígitos como 000.000.000-00.
func formatCPFDigits(digits string) string {
	if len(digits) != 11 {
		return digits
	}
	return digits[0:3] + "." + digits[3:6] + "." + digits[6:9] + "-" + digits[9:11]
}

// GetCheckinCPFLookupHandler — GET /org/:slug/events/:id/checkin-data/lookup?cpf=
//
// Busca os ingressos do CPF informado dentro do evento da URL. Staff pode usar
// isso quando o participante não tem o QR impresso à mão.
//
// Devolve uma lista porque a mesma pessoa pode ter mais de um ingresso no mesmo
// evento (lotes diferentes, ou quantity > 1). O front confirma um por vez.
//
// Ingressos cancelados entram na resposta com status próprio: o staff precisa
// ver que o ingresso existe e está cancelado, e não simplesmente "não achei".
func GetCheckinCPFLookupHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid, _ := userID.(string)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, ok := resolveCheckinOrg(ctx, db, orgSlug, uid)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	if !eventBelongsToOrg(ctx, db, eventID, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	cpfDigits := cleanCPFDigits(c.Query("cpf"))
	if len(cpfDigits) != 11 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "CPF inválido",
			"code":  "invalid_cpf",
		})
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
		  t.id,
		  t.qr_code,
		  t.status,
		  t.checked_in_at,
		  t.checked_in_by,
		  tb.name   AS batch_name,
		  tb.type   AS batch_type,
		  o.id      AS order_id,
		  u.id      AS user_id,
		  u.full_name,
		  u.email,
		  u.cpf,
		  u.avatar_url
		FROM tickets t
		JOIN orders o          ON o.id  = t.order_id
		JOIN users  u          ON u.id  = t.user_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE o.event_id = $1
		  AND o.status   = 'paid'
		  AND u.cpf_digits = $2
		ORDER BY tb.position ASC, t.created_at ASC`, eventID, cpfDigits,
	)
	if err != nil {
		log.Printf("GetCheckinCPFLookupHandler query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar CPF"})
		return
	}
	defer rows.Close()

	tickets := []gin.H{}
	for rows.Next() {
		var t checkinTicket
		if err := rows.Scan(
			&t.ID, &t.QRCode, &t.Status, &t.CheckedInAt, &t.CheckedInBy,
			&t.BatchName, &t.BatchType,
			&t.OrderID,
			&t.UserID,
			&t.FullName, &t.Email, &t.CPF, &t.AvatarURL,
		); err != nil {
			log.Printf("GetCheckinCPFLookupHandler scan: %v", err)
			continue
		}
		tickets = append(tickets, t.payload())
	}
	if err := rows.Err(); err != nil {
		log.Printf("GetCheckinCPFLookupHandler rows: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar CPF"})
		return
	}

	if len(tickets) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "nenhum ingresso encontrado para este CPF",
			"code":    "not_found",
			"tickets": []gin.H{},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"cpf":     formatCPFDigits(cpfDigits),
		"tickets": tickets,
	})
}

// PatchCheckinHandler — PATCH /org/:slug/events/:id/checkin-data
// Faz ou desfaz o check-in de um ingresso, localizado pelo token do QR code —
// o mesmo que sai impresso no QR e no PDF oficial. Aceita tanto o token lido
// por scanner quanto o digitado a mao pelo staff.
func PatchCheckinHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid, _ := userID.(string)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	var body struct {
		QRCode    string `json:"qr_code"`
		CheckedIn bool   `json:"checked_in"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	qrCode := strings.ToUpper(strings.TrimSpace(body.QRCode))
	if !validTicketQRCode(qrCode) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "QR code inválido",
			"code":  "invalid_qr",
		})
		return
	}

	db := config.GetDB()
	ctx := c.Request.Context()

	orgID, ok := resolveCheckinOrg(ctx, db, orgSlug, uid)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	if !eventBelongsToOrg(ctx, db, eventID, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	ticket, err := loadTicketByQRCode(ctx, db, qrCode, eventID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "ingresso não encontrado para este evento",
			"code":  "not_found",
		})
		return
	} else if err != nil {
		log.Printf("PatchCheckinHandler loadTicketByQRCode: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	// Reembolsos cancelam o ingresso sem alterar o status do pedido
	// (ver client.RequestRefund), então o status precisa ser checado aqui.
	switch ticket.Status {
	case ticketStatusValid, ticketStatusUsed:
		// segue para o toggle

	case ticketStatusCancelled:
		c.JSON(http.StatusConflict, gin.H{
			"error":  "ingresso cancelado",
			"code":   "ticket_cancelled",
			"ticket": ticket.payload(),
		})
		return

	default:
		c.JSON(http.StatusConflict, gin.H{
			"error":  "ingresso indisponível para check-in",
			"code":   "invalid_ticket_status",
			"status": ticket.Status,
			"ticket": ticket.payload(),
		})
		return
	}

	if body.CheckedIn {
		if ticket.Status == ticketStatusUsed {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "este ingresso já teve check-in",
				"code":   "already_checked_in",
				"ticket": ticket.payload(),
			})
			return
		}

		// A guarda de status no WHERE faz a checagem e a escrita em uma só
		// instrução: dois balcões varrendo o mesmo QR não geram dois check-ins.
		res, err := db.ExecContext(ctx, `
			UPDATE tickets SET
			  status        = 'used',
			  checked_in_at = NOW(),
			  checked_in_by = $1
			WHERE id = $2
			  AND status = 'valid'`,
			uid, ticket.ID,
		)
		if err != nil {
			log.Printf("PatchCheckinHandler update: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar check-in"})
			return
		}

		affected, err := res.RowsAffected()
		if err != nil {
			log.Printf("PatchCheckinHandler RowsAffected: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar check-in"})
			return
		}
		if affected == 0 {
			// Outro balcão leu o mesmo QR antes deste UPDATE.
			c.JSON(http.StatusConflict, gin.H{
				"error": "este ingresso acabou de receber check-in",
				"code":  "already_checked_in",
			})
			return
		}

		ticket.Status      = ticketStatusUsed
		ticket.CheckedInAt = sql.NullTime{Time: time.Now(), Valid: true}
		ticket.CheckedInBy = sql.NullString{String: uid, Valid: true}
	} else {
		// Desfazer: staff corrigiu uma marcação indevida.
		res, err := db.ExecContext(ctx, `
			UPDATE tickets SET
			  status        = 'valid',
			  checked_in_at = NULL,
			  checked_in_by = NULL
			WHERE id = $1
			  AND status = 'used'`,
			ticket.ID,
		)
		if err != nil {
			log.Printf("PatchCheckinHandler undo: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar check-in"})
			return
		}

		affected, err := res.RowsAffected()
		if err != nil {
			log.Printf("PatchCheckinHandler RowsAffected undo: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar check-in"})
			return
		}
		if affected == 0 {
			c.JSON(http.StatusConflict, gin.H{
				"error": "ingresso não estava marcado como check-in",
				"code":  "not_checked_in",
			})
			return
		}

		ticket.Status      = ticketStatusValid
		ticket.CheckedInAt = sql.NullTime{}
		ticket.CheckedInBy = sql.NullString{}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"checkedIn": body.CheckedIn,
		"ticket":    ticket.payload(),
	})
}