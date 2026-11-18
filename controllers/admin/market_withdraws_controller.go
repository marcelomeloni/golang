package admin

import (
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

type MarketWithdrawRequest struct {
	ID          string     `json:"id"`
	SellerID    string     `json:"seller_id"`
	SellerName  string     `json:"seller_name"`
	Amount      float64    `json:"amount"`
	PixKey      *string    `json:"pix_key"`
	PixKeyType  *string    `json:"pix_key_type"`
	Status      string     `json:"status"`
	AdminNote   *string    `json:"admin_note"`
	ExternalRef *string    `json:"external_ref"`
	CreatedAt   time.Time  `json:"created_at"`
	ReviewedAt  *time.Time `json:"reviewed_at"`
	PaidAt      *time.Time `json:"paid_at"`
}

func ListMarketWithdraws(c *gin.Context) {
	status := c.Query("status")
	where := ""
	args := []interface{}{}
	if status != "" {
		where = " WHERE w.status = $1"
		args = append(args, status)
	}

	rows, err := config.GetDB().Query(`
		SELECT w.id, w.seller_id, COALESCE(u.full_name, ''), w.amount,
		       w.pix_key, w.pix_key_type, w.status, w.admin_note, w.external_ref,
		       w.created_at, w.reviewed_at, w.paid_at
		FROM market_withdraw_requests w
		LEFT JOIN users u ON u.id = w.seller_id`+where+`
		ORDER BY CASE w.status WHEN 'pending' THEN 0 ELSE 1 END, w.created_at DESC`, args...)
	if err != nil {
		log.Printf("ListMarketWithdraws: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar saques"})
		return
	}
	defer rows.Close()

	list := []MarketWithdrawRequest{}
	for rows.Next() {
		var (
			item                          MarketWithdrawRequest
			pixKey, pixKeyType, adminNote, externalRef sql.NullString
			reviewedAt, paidAt                         sql.NullTime
		)
		if err := rows.Scan(
			&item.ID, &item.SellerID, &item.SellerName, &item.Amount,
			&pixKey, &pixKeyType, &item.Status, &adminNote, &externalRef,
			&item.CreatedAt, &reviewedAt, &paidAt,
		); err != nil {
			log.Printf("ListMarketWithdraws scan: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar saques"})
			return
		}
		item.PixKey = nullStr(pixKey)
		item.PixKeyType = nullStr(pixKeyType)
		item.AdminNote = nullStr(adminNote)
		item.ExternalRef = nullStr(externalRef)
		item.ReviewedAt = nullTime(reviewedAt)
		item.PaidAt = nullTime(paidAt)
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("ListMarketWithdraws rows: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{"withdraws": list})
}

type ReviewMarketWithdrawRequest struct {
	AdminNote   string `json:"admin_note"`
	ExternalRef string `json:"external_ref"`
}

func PayMarketWithdraw(c *gin.Context) {
	reviewMarketWithdraw(c, "paid")
}

func RejectMarketWithdraw(c *gin.Context) {
	reviewMarketWithdraw(c, "rejected")
}

func reviewMarketWithdraw(c *gin.Context, status string) {
	requestID := c.Param("id")

	var req ReviewMarketWithdrawRequest
	_ = c.ShouldBindJSON(&req)

	reviewedBy, _ := c.Get("userID")
	uid, _ := reviewedBy.(string)

	externalRef := strings.TrimSpace(req.ExternalRef)
	if status == "paid" && externalRef == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "informe o ID/comprovante da transferência"})
		return
	}

	db := config.GetDB()
	tx, err := db.Begin()
	if err != nil {
		log.Printf("reviewMarketWithdraw begin: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar"})
		return
	}
	defer tx.Rollback()

	var current, sellerID string
	var amount float64
	err = tx.QueryRow(`
		SELECT status, seller_id, amount FROM market_withdraw_requests WHERE id = $1
	`, requestID).Scan(&current, &sellerID, &amount)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "solicitação de saque não encontrada"})
			return
		}
		log.Printf("reviewMarketWithdraw lookup: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar"})
		return
	}
	if current != "pending" {
		c.JSON(http.StatusConflict, gin.H{"error": "esta solicitação já foi processada", "status": current})
		return
	}

	if status == "paid" {
		_, err = tx.Exec(`
			UPDATE market_withdraw_requests
			SET status = 'paid', admin_note = NULLIF($2, ''), reviewed_by = $3,
			    external_ref = $4, reviewed_at = NOW(), paid_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'pending'
		`, requestID, req.AdminNote, uid, externalRef)
		if err == nil {
			_, err = tx.Exec(`
				UPDATE market_transactions
				SET escrow_status = 'released', released_at = NOW()
				WHERE withdraw_request_id = $1
			`, requestID)
		}
		if err == nil {
			_, err = tx.Exec(`
				UPDATE users SET market_balance = GREATEST(market_balance - $1, 0::numeric) WHERE id = $2
			`, amount, sellerID)
		}
	} else {
		_, err = tx.Exec(`
			UPDATE market_withdraw_requests
			SET status = 'rejected', admin_note = NULLIF($2, ''), reviewed_by = $3,
			    reviewed_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND status = 'pending'
		`, requestID, req.AdminNote, uid)
		if err == nil {
			_, err = tx.Exec(`
				UPDATE market_transactions
				SET escrow_status = 'held', withdraw_request_id = NULL
				WHERE withdraw_request_id = $1
			`, requestID)
		}
	}
	if err != nil {
		log.Printf("reviewMarketWithdraw update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar solicitação"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("reviewMarketWithdraw commit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar solicitação"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "solicitação atualizada", "status": status})
}
