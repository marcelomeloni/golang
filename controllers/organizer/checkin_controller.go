package organizer

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

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

	if _, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
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