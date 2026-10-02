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

type SalesSummary struct {
	TotalWithdrawable float64 `json:"totalWithdrawable"`
	TotalPending      float64 `json:"totalPending"`
	TotalReleased     float64 `json:"totalReleased"`
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
	summary := SalesSummary{}

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

		// Popula o resumo financeiro para a UI
		if escrowStatus == "released" {
			summary.TotalReleased += netAmount
		} else if canWithdraw {
			summary.TotalWithdrawable += netAmount
		} else if escrowStatus == "held" || escrowStatus == "pending" {
			summary.TotalPending += netAmount
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

	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"sales":   sales,
	})
}

// WithdrawBalance saca TODAS as vendas elegíveis de uma só vez
func WithdrawBalance(c *gin.Context) {
	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)
	if userIDStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	db := config.GetDB()

	// 1. Verifica se o usuário tem chave PIX cadastrada
	var pixKey, pixKeyType sql.NullString
	err := db.QueryRow(`SELECT pix_key, pix_key_type FROM users WHERE id = $1`, userIDStr).Scan(&pixKey, &pixKeyType)
	if err != nil {
		log.Printf("WithdrawBalance user scan: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno ao buscar usuário"})
		return
	}

	if !pixKey.Valid || pixKey.String == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "cadastre sua chave PIX antes de sacar",
			"code":  "missing_pix_key",
		})
		return
	}

	// 2. Busca todas as transações elegíveis para este usuário
	rows, err := db.Query(`
		SELECT 
			mt.id,
			mt.amount,
			mt.platform_fee,
			mt.escrow_status,
			mt.held_at,
			e.start_date
		FROM market_transactions mt
		JOIN market_listings ml ON ml.id = mt.listing_id
		JOIN events e           ON e.id  = ml.event_id
		WHERE ml.seller_id = $1
		  AND mt.escrow_status = 'held'
	`, userIDStr)
	if err != nil {
		log.Printf("WithdrawBalance query eligible: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar saldo"})
		return
	}
	defer rows.Close()

	var eligibleTransactionIDs []string
	var totalPayout float64 = 0.0

	// 3. Filtra via código para usar a mesma regra de negócio da visualização
	for rows.Next() {
		var txID string
		var amount, platformFee float64
		var escrowStatus string
		var heldAt sql.NullTime
		var eventDate time.Time

		if err := rows.Scan(&txID, &amount, &platformFee, &escrowStatus, &heldAt, &eventDate); err != nil {
			log.Printf("WithdrawBalance row scan: %v", err)
			continue
		}

		canWithdraw, _ := evalWithdrawEligibility(escrowStatus, eventDate, heldAt)
		if canWithdraw {
			eligibleTransactionIDs = append(eligibleTransactionIDs, txID)
			totalPayout += (amount - platformFee)
		}
	}

	if len(eligibleTransactionIDs) == 0 || totalPayout <= 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "nenhum saldo elegível para saque no momento"})
		return
	}

	// 4. Cria um ID único para este lote de pagamento (para os logs e gateway)
	batchRefID := fmt.Sprintf("batch_withdraw_%s_%d", userIDStr, time.Now().Unix())

	// 5. Executa o Payout no Gateway (UMA ÚNICA VEZ)
	// Adapte o `Withdraw` caso ele precise do ID transacional, mandaremos o batchRefID
	if err := paymentservice.Default.Withdraw(batchRefID, totalPayout, pixKey.String, pixKeyType.String); err != nil {
		log.Printf("WithdrawBalance payout falhou para o lote %s: %v", batchRefID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar saque com o banco"})
		return
	}

	// 6. Atualiza todas as transações em uma única Transaction segura no DB
	tx, err := db.Begin()
	if err != nil {
		log.Printf("WithdrawBalance db begin tx: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "saque processado, mas erro ao atualizar status"})
		return
	}

	for _, id := range eligibleTransactionIDs {
		_, err := tx.Exec(`
			UPDATE market_transactions 
			SET escrow_status = 'released', released_at = NOW() 
			WHERE id = $1
		`, id)
		if err != nil {
			log.Printf("WithdrawBalance tx update failed for id %s: %v", id, err)
			tx.Rollback()
			// Idealmente você tem uma fila de retentativas aqui se o banco cair, pois o PIX já foi enviado.
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro de consistência"})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("WithdrawBalance tx commit: %v", err)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"withdrawnAmount":   totalPayout,
		"transactionsCount": len(eligibleTransactionIDs),
		"pixKey":            pixKey.String,
	})
}

// As regras continuam intactas
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

	// Regra 1: Passou de 48h antes do evento
	if time.Until(eventDate).Hours() < 48 {
		return true, ""
	}

	// Regra 2: Passou de 7 dias desde a compra
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