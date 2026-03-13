package client

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/paymentservice"
	"github.com/gin-gonic/gin"
)

type MySaleItem struct {
	TransactionID string  `json:"transactionId"`
	EventTitle    string  `json:"eventTitle"`
	EventDate     string  `json:"eventDate"`
	LotTitle      string  `json:"lotTitle"`
	Amount        float64 `json:"amount"`
	PlatformFee   float64 `json:"platformFee"`
	NetAmount     float64 `json:"netAmount"`
	EscrowStatus  string  `json:"escrowStatus"`
	CanWithdraw   bool    `json:"canWithdraw"`
	WithdrawBlock string  `json:"withdrawBlock"`
	HeldAt        string  `json:"heldAt"`
}

func GetMySales(c *gin.Context) {
	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)
	if userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	db := config.GetDB()

	rows, err := db.Query(`
		WITH ranked AS (
			SELECT
				mt.id,
				e.title        AS event_title,
				e.start_date   AS event_date,
				tb.name        AS lot_title,
				mt.amount,
				mt.platform_fee,
				mt.escrow_status,
				mt.held_at,
				mt.created_at,
				ROW_NUMBER() OVER (
					PARTITION BY mt.listing_id
					ORDER BY
						CASE mt.escrow_status
							WHEN 'held'     THEN 1
							WHEN 'released' THEN 2
							WHEN 'pending'  THEN 3
							ELSE                 4
						END,
						mt.created_at DESC
				) AS rn
			FROM market_transactions mt
			JOIN market_listings ml ON ml.id = mt.listing_id
			JOIN events e           ON e.id  = ml.event_id
			JOIN tickets t          ON t.id  = ml.ticket_id
			JOIN ticket_batches tb  ON tb.id = t.batch_id
			WHERE ml.seller_id = $1
			  AND mt.escrow_status NOT IN ('cancelled', 'refunded')
		)
		SELECT id, event_title, event_date, lot_title,
		       amount, platform_fee, escrow_status, held_at
		FROM ranked
		WHERE rn = 1
		ORDER BY created_at DESC
	`, userIDStr)
	if err != nil {
		log.Printf("GetMySales query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer rows.Close()

	sales := []MySaleItem{}
	for rows.Next() {
		var (
			txID         string
			eventTitle   string
			eventDate    time.Time
			lotTitle     string
			amount       float64
			platformFee  float64
			escrowStatus string
			heldAt       sql.NullTime
		)

		if err := rows.Scan(&txID, &eventTitle, &eventDate, &lotTitle,
			&amount, &platformFee, &escrowStatus, &heldAt); err != nil {
			log.Printf("GetMySales scan: %v", err)
			continue
		}

		netAmount := amount - platformFee
		canWithdraw, blockReason := evalWithdrawEligibility(escrowStatus, eventDate, heldAt)

		heldAtStr := ""
		if heldAt.Valid {
			heldAtStr = heldAt.Time.Format(time.RFC3339)
		}

		sales = append(sales, MySaleItem{
			TransactionID: txID,
			EventTitle:    eventTitle,
			EventDate:     eventDate.Format(time.RFC3339),
			LotTitle:      lotTitle,
			Amount:        amount,
			PlatformFee:   platformFee,
			NetAmount:     netAmount,
			EscrowStatus:  escrowStatus,
			CanWithdraw:   canWithdraw,
			WithdrawBlock: blockReason,
			HeldAt:        heldAtStr,
		})
	}

	c.JSON(http.StatusOK, gin.H{"sales": sales})
}

func WithdrawSale(c *gin.Context) {
	transactionID := c.Param("transactionId")

	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)
	if userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	db := config.GetDB()

	var (
		eventDate    time.Time
		amount       float64
		platformFee  float64
		escrowStatus string
		heldAt       sql.NullTime
		pixKey       sql.NullString
		pixKeyType   sql.NullString
		sellerID     string
	)

	err := db.QueryRow(`
		SELECT
			e.start_date,
			mt.amount,
			mt.platform_fee,
			mt.escrow_status,
			mt.held_at,
			u.pix_key,
			u.pix_key_type,
			ml.seller_id
		FROM market_transactions mt
		JOIN market_listings ml ON ml.id = mt.listing_id
		JOIN events e           ON e.id  = ml.event_id
		JOIN users u            ON u.id  = ml.seller_id
		WHERE mt.id = $1
	`, transactionID).Scan(
		&eventDate, &amount, &platformFee, &escrowStatus, &heldAt,
		&pixKey, &pixKeyType, &sellerID,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "transação não encontrada"})
		return
	}
	if err != nil {
		log.Printf("WithdrawSale scan: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	if sellerID != userIDStr {
		c.JSON(http.StatusForbidden, gin.H{"error": "esta venda não pertence a você"})
		return
	}

	canWithdraw, blockReason := evalWithdrawEligibility(escrowStatus, eventDate, heldAt)
	if !canWithdraw {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": blockReason})
		return
	}

	if !pixKey.Valid || pixKey.String == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "cadastre sua chave PIX antes de sacar",
			"code":  "missing_pix_key",
		})
		return
	}

	netAmount := amount - platformFee

	if err := paymentservice.Default.Withdraw(transactionID, netAmount, pixKey.String, pixKeyType.String); err != nil {
		log.Printf("WithdrawSale payout transactionID=%s: %v", transactionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar saque"})
		return
	}

	_, err = db.Exec(`
		UPDATE market_transactions
		SET escrow_status = 'released', released_at = NOW()
		WHERE id = $1
	`, transactionID)
	if err != nil {
		log.Printf("WithdrawSale update escrow transactionID=%s: %v", transactionID, err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"netAmount": netAmount,
		"pixKey":    pixKey.String,
	})
}

func evalWithdrawEligibility(escrowStatus string, eventDate time.Time, heldAt sql.NullTime) (bool, string) {
	if escrowStatus == "released" {
		return false, "saque já realizado"
	}
	if escrowStatus == "cancelled" || escrowStatus == "refunded" {
		return false, "venda cancelada ou reembolsada"
	}
	if escrowStatus != "held" {
		return false, "pagamento ainda não confirmado"
	}

	now := time.Now()

	if time.Until(eventDate).Hours() < 48 {
		return true, ""
	}

	if heldAt.Valid && now.Sub(heldAt.Time) >= 7*24*time.Hour {
		return true, ""
	}

	daysLeft := 7 - int(now.Sub(heldAt.Time).Hours()/24)
	return false, formatWithdrawBlockReason(eventDate, daysLeft)
}

func formatWithdrawBlockReason(eventDate time.Time, daysLeft int) string {
	if time.Until(eventDate).Hours() > 48 {
		if daysLeft > 1 {
			return fmt.Sprintf("saque disponível em %d dias ou 48h antes do evento", daysLeft)
		}
		return "saque disponível amanhã ou 48h antes do evento"
	}
	return "saque disponível 48h antes do evento"
}