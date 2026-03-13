package client

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/orderservice"
	"bilheteria-api/services/paymentservice"
	"github.com/gin-gonic/gin"
)

const marketPlatformFee = 1.6

func CreateMarketOrder(c *gin.Context) {
	var req CreateMarketOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "listingId é obrigatório"})
		return
	}

	db := config.GetDB()

	listing, err := loadActiveListing(db, req.ListingID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não disponível no mercado"})
		return
	} else if err != nil {
		log.Printf("CreateMarketOrder loadActiveListing: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)
	isGuest := userIDStr == ""

	guest := &orderservice.GuestInfo{
		Name:  req.BuyerName,
		Email: req.BuyerEmail,
		CPF:   req.BuyerCPF,
	}

	buyerID, conflict, err := orderservice.ResolveUserID(db, userIDStr, guest)
	if err != nil {
		log.Printf("CreateMarketOrder ResolveUserID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao identificar comprador"})
		return
	}
	if conflict != nil {
		c.JSON(http.StatusConflict, gin.H{"error": resolveConflictMessage(conflict), "code": conflict.Code})
		return
	}

	total := listing.price + marketPlatformFee

	tx, err := db.Begin()
	if err != nil {
		log.Printf("CreateMarketOrder begin tx: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	orderID, err := createMarketOrder(tx, listing.eventID, buyerID, total, marketPlatformFee)
	if err != nil {
		log.Printf("CreateMarketOrder createMarketOrder: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	transactionID, err := createMarketTransaction(tx, req.ListingID, buyerID, orderID, total, marketPlatformFee)
	if err != nil {
		log.Printf("CreateMarketOrder createMarketTransaction: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("CreateMarketOrder commit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	pixResult, err := paymentservice.Default.GeneratePix(
		orderID, total, req.BuyerName, req.BuyerEmail, req.BuyerCPF, "",
	)
	if err != nil {
		log.Printf("CreateMarketOrder GeneratePix orderID=%s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar cobrança PIX"})
		return
	}

	if err := orderservice.SavePixCharge(db, orderID, pixResult.ExternalID); err != nil {
		log.Printf("CreateMarketOrder SavePixCharge orderID=%s: %v", orderID, err)
	}

	c.JSON(http.StatusAccepted, CreateMarketOrderResponse{
		TransactionID:     transactionID,
		PaymentMethod:     "pix",
		TotalAmount:       total,
		IsGuest:           isGuest,
		ConfirmationEmail: req.BuyerEmail,
		PixCode:           pixResult.PixCode,
		PixQrCode:         pixResult.PixQrCode,
	})
}

func CheckMarketPixStatus(c *gin.Context) {
	transactionID := c.Param("transactionId")

	db := config.GetDB()

	var orderID string
	err := db.QueryRow(`
		SELECT order_id FROM market_transactions WHERE id = $1
	`, transactionID).Scan(&orderID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "transação não encontrada"})
		return
	} else if err != nil {
		log.Printf("CheckMarketPixStatus scan: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	externalID, err := orderservice.GetPixExternalID(db, orderID)
	if err != nil {
		log.Printf("CheckMarketPixStatus GetPixExternalID orderID=%s: %v", orderID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "cobrança não encontrada"})
		return
	}

	status, err := paymentservice.Default.CheckStatus(externalID)
	if err != nil {
		log.Printf("CheckMarketPixStatus CheckStatus: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao consultar status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"transactionId": transactionID,
		"status":        status,
	})
}

type listingInfo struct {
	eventID  string
	ticketID string
	sellerID string
	batchID  string
	price    float64
}

func loadActiveListing(db *sql.DB, listingID string) (listingInfo, error) {
	var l listingInfo
	err := db.QueryRow(`
		SELECT m.event_id, m.ticket_id, m.seller_id, t.batch_id, m.price
		FROM market_listings m
		JOIN tickets t ON t.id = m.ticket_id
		WHERE m.id = $1
		  AND m.status = 'active'
		  AND t.status = 'valid'
	`, listingID).Scan(&l.eventID, &l.ticketID, &l.sellerID, &l.batchID, &l.price)
	return l, err
}

func createMarketOrder(tx *sql.Tx, eventID string, buyerID sql.NullString, total, platformFee float64) (string, error) {
	var orderID string
	err := tx.QueryRow(`
		INSERT INTO orders
		  (event_id, user_id, total_amount, platform_fee_amount, net_amount,
		   status, payment_method, order_type)
		VALUES ($1, $2, $3, $4, $5, 'pending', 'pix', 'market')
		RETURNING id
	`, eventID, buyerID, total, platformFee, total-platformFee).Scan(&orderID)
	return orderID, err
}

func createMarketTransaction(
	tx *sql.Tx,
	listingID string,
	buyerID sql.NullString,
	orderID string,
	amount, platformFee float64,
) (string, error) {
	var txID string
	err := tx.QueryRow(`
		INSERT INTO market_transactions
		  (listing_id, buyer_id, order_id, new_ticket_id, amount, platform_fee, escrow_status)
		VALUES ($1, $2, $3, NULL, $4, $5, 'pending')
		RETURNING id
	`, listingID, buyerID, orderID, amount, platformFee).Scan(&txID)
	return txID, err
}

func resolveConflictMessage(conflict *orderservice.ConflictInfo) string {
	switch conflict.Code {
	case "cpf_already_exists":
		return fmt.Sprintf("CPF já cadastrado no e-mail %s. Faça login para continuar.", conflict.MaskedEmail)
	case "email_already_exists":
		return fmt.Sprintf("E-mail %s já cadastrado. Faça login para continuar.", conflict.MaskedEmail)
	}
	return "conflito de dados. Tente novamente."
}