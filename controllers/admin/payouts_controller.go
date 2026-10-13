package admin

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// PayoutRequest é um pedido de repasse visto pelo admin.
type PayoutRequest struct {
	ID              string     `json:"id"`
	OrganizationID  string     `json:"organization_id"`
	OrgName         string     `json:"org_name"`
	OrgSlug         string     `json:"org_slug"`
	RequestedByName string     `json:"requested_by_name"`
	Amount          float64    `json:"amount"`
	Status          string     `json:"status"`
	BankAccountID   *string    `json:"bank_account_id"`
	PixKey          *string    `json:"pix_key"`
	PixKeyType      *string    `json:"pix_key_type"`
	HolderName      *string    `json:"holder_name"`
	HolderDocument  *string    `json:"holder_document"`
	Note            *string    `json:"note"`
	AdminNote       *string    `json:"admin_note"`
	CreatedAt       time.Time  `json:"created_at"`
	ReviewedAt      *time.Time `json:"reviewed_at"`
	PaidAt          *time.Time `json:"paid_at"`
}

func nullStr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// ListPayouts devolve os pedidos de repasse, opcionalmente filtrados por status.
func ListPayouts(c *gin.Context) {
	status := c.Query("status")

	where := ""
	args := []interface{}{}
	if status != "" {
		where = " WHERE p.status = $1"
		args = append(args, status)
	}

	rows, err := config.GetDB().Query(`
		SELECT
			p.id, p.organization_id, o.name, o.slug,
			COALESCE(u.full_name, ''),
			p.amount, p.status,
			p.bank_account_id, p.pix_key, p.pix_key_type,
			p.holder_name, p.holder_document, p.note,
			p.admin_note, p.created_at, p.reviewed_at, p.paid_at
		FROM payout_requests p
		JOIN organizations o ON o.id = p.organization_id
		LEFT JOIN users u ON u.id = p.requested_by`+where+`
		ORDER BY
			CASE p.status WHEN 'pending' THEN 0 ELSE 1 END,
			p.created_at DESC`, args...)
	if err != nil {
		log.Printf("ListPayouts: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar repasses"})
		return
	}
	defer rows.Close()

	list := []PayoutRequest{}
	for rows.Next() {
		var (
			p                                 PayoutRequest
			bankAccountID, pixKey, pixKeyType sql.NullString
			holderName, holderDocument        sql.NullString
			note, adminNote                   sql.NullString
			reviewedAt, paidAt                sql.NullTime
		)
		if err := rows.Scan(
			&p.ID, &p.OrganizationID, &p.OrgName, &p.OrgSlug,
			&p.RequestedByName, &p.Amount, &p.Status,
			&bankAccountID, &pixKey, &pixKeyType,
			&holderName, &holderDocument, &note, &adminNote,
			&p.CreatedAt, &reviewedAt, &paidAt,
		); err != nil {
			log.Printf("ListPayouts scan: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar repasses"})
			return
		}
		p.BankAccountID = nullStr(bankAccountID)
		p.PixKey = nullStr(pixKey)
		p.PixKeyType = nullStr(pixKeyType)
		p.HolderName = nullStr(holderName)
		p.HolderDocument = nullStr(holderDocument)
		p.Note = nullStr(note)
		p.AdminNote = nullStr(adminNote)
		p.ReviewedAt = nullTime(reviewedAt)
		p.PaidAt = nullTime(paidAt)
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ListPayouts rows: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"payouts": list})
}

type ReviewPayoutRequest struct {
	AdminNote string `json:"admin_note"`
}

// PayPayout marca um pedido de repasse como pago (pagamento manual via PIX).
func PayPayout(c *gin.Context) {
	reviewPayout(c, "paid")
}

// RejectPayout rejeita um pedido de repasse.
func RejectPayout(c *gin.Context) {
	reviewPayout(c, "rejected")
}

// reviewPayout transiciona um pedido pendente para pago/rejeitado.
func reviewPayout(c *gin.Context, status string) {
	payoutID := c.Param("id")

	var req ReviewPayoutRequest
	_ = c.ShouldBindJSON(&req)

	reviewedBy, _ := c.Get("userID")
	uid, _ := reviewedBy.(string)

	db := config.GetDB()

	var current string
	if err := db.QueryRow(`SELECT status FROM payout_requests WHERE id = $1`, payoutID).Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "pedido de repasse não encontrado"})
			return
		}
		log.Printf("reviewPayout lookup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar"})
		return
	}
	if current != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "este pedido já foi processado", "status": current})
		return
	}

	var paidAt sql.NullTime
	if status == "paid" {
		paidAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	res, err := db.Exec(`
		UPDATE payout_requests
		SET status = $1,
		    admin_note = NULLIF($2, ''),
		    reviewed_by = $3,
		    reviewed_at = NOW(),
		    paid_at = $4,
		    updated_at = NOW()
		WHERE id = $5 AND status = 'pending'`,
		status, req.AdminNote, uid, paidAt, payoutID,
	)
	if err != nil {
		log.Printf("reviewPayout update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar pedido"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "este pedido já foi processado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "pedido atualizado", "status": status})
}
