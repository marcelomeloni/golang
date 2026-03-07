package client

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/orderservice"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ──────────────────────────────────────────────
// Reppy Market — Listar oferta
// ──────────────────────────────────────────────

type CreateListingRequest struct {
	Price float64 `json:"price" binding:"required,gt=0"`
}

// POST /tickets/:id/market
// Cadastra o ingresso no Reppy Market com validação de preço por categoria.
func CreateMarketListing(c *gin.Context) {
	userID, _ := c.Get("userID")
	ticketID := c.Param("id")

	var req CreateListingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preço inválido"})
		return
	}

	db := config.GetDB()

	// Verifica que o ingresso pertence ao usuário, está ativo,
	// e coleta event_id, allow_reppy_market e category_id do lote.
	var eventID string
	var allowMarketEvent bool
	var allowMarketCategory sql.NullBool
	var categoryID sql.NullString
	err := db.QueryRow(`
		SELECT
			e.id,
			COALESCE(e.allow_reppy_market, false),
			tc.in_reppy_market,
			tb.category_id
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		JOIN events e ON e.id = o.event_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		LEFT JOIN ticket_categories tc ON tc.id = tb.category_id
		WHERE t.id = $1
		  AND t.user_id = $2
		  AND t.status = 'valid'
		  AND o.status = 'paid'
		  AND e.status = 'published'
		  AND e.end_date > NOW()
	`, ticketID, userID).Scan(&eventID, &allowMarketEvent, &allowMarketCategory, &categoryID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não encontrado ou não elegível"})
		return
	}
	if err != nil {
		log.Printf("CreateMarketListing query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	// Bloqueia se o evento desativou o Market
	if !allowMarketEvent {
		c.JSON(http.StatusForbidden, gin.H{"error": "o organizador desativou o Reppy Market para este evento"})
		return
	}
	// Bloqueia se a categoria desativou o Market (in_reppy_market = false)
	if allowMarketCategory.Valid && !allowMarketCategory.Bool {
		c.JSON(http.StatusForbidden, gin.H{"error": "esta categoria de ingresso não permite venda no Reppy Market"})
		return
	}

	// Bloqueia ingresso gratuito — não faz sentido revender por preço < 0
	if req.Price <= 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "ingressos gratuitos não podem ser vendidos no Reppy Market"})
		return
	}

	// Regra central: preço deve ser menor que o menor lote ativo da mesma categoria.
	currentMin, err := activeMinPriceByCategory(db, eventID, categoryID)
	if err != nil || currentMin == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "nenhum lote ativo encontrado para esta categoria"})
		return
	}

	if req.Price >= currentMin {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "preço deve ser menor que o lote atual da categoria",
			"maxPrice": currentMin - 0.01,
		})
		return
	}

	// Garante que não existe listagem ativa para este ingresso
	var existingCount int
	db.QueryRow(`
		SELECT COUNT(*) FROM market_listings
		WHERE ticket_id = $1 AND status = 'active'
	`, ticketID).Scan(&existingCount)
	if existingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "ingresso já está listado no Reppy Market"})
		return
	}

	listingID := uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO market_listings (id, event_id, ticket_id, seller_id, price, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'active', NOW(), NOW())
	`, listingID, eventID, ticketID, userID, req.Price)
	if err != nil {
		log.Printf("CreateMarketListing insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar listagem"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": listingID, "price": req.Price})
}

// ──────────────────────────────────────────────
// Reppy Market — Editar oferta
// ──────────────────────────────────────────────

type UpdateListingRequest struct {
	Price float64 `json:"price" binding:"required,gt=0"`
}

// PATCH /market/listings/:listingId
func UpdateMarketListing(c *gin.Context) {
	userID, _ := c.Get("userID")
	listingID := c.Param("listingId")

	var req UpdateListingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "preço inválido"})
		return
	}

	db := config.GetDB()

	// Busca event_id e category_id via listagem → ticket → lote
	var eventID string
	var categoryID sql.NullString
	err := db.QueryRow(`
		SELECT e.id, tb.category_id
		FROM market_listings ml
		JOIN tickets t ON t.id = ml.ticket_id
		JOIN orders o ON o.id = t.order_id
		JOIN events e ON e.id = o.event_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE ml.id = $1
		  AND ml.seller_id = $2
		  AND ml.status = 'active'
	`, listingID, userID).Scan(&eventID, &categoryID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "listagem não encontrada"})
		return
	}
	if err != nil {
		log.Printf("UpdateMarketListing query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	// Mesma regra: preço < menor lote ativo da categoria
	currentMin, err := activeMinPriceByCategory(db, eventID, categoryID)
	if err != nil || currentMin == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "nenhum lote ativo encontrado para esta categoria"})
		return
	}

	if req.Price >= currentMin {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "preço deve ser menor que o lote atual da categoria",
			"maxPrice": currentMin - 0.01,
		})
		return
	}

	_, err = db.Exec(`
		UPDATE market_listings SET price = $1, updated_at = NOW()
		WHERE id = $2 AND seller_id = $3
	`, req.Price, listingID, userID)
	if err != nil {
		log.Printf("UpdateMarketListing update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar listagem"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": listingID, "price": req.Price})
}

// ──────────────────────────────────────────────
// Reppy Market — Deletar oferta
// ──────────────────────────────────────────────

// DELETE /market/listings/:listingId
func DeleteMarketListing(c *gin.Context) {
	userID, _ := c.Get("userID")
	listingID := c.Param("listingId")

	db := config.GetDB()

	result, err := db.Exec(`
		UPDATE market_listings SET status = 'cancelled', updated_at = NOW()
		WHERE id = $1 AND seller_id = $2 AND status = 'active'
	`, listingID, userID)
	if err != nil {
		log.Printf("DeleteMarketListing: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover listagem"})
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "listagem não encontrada ou já removida"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "listagem removida"})
}

// ──────────────────────────────────────────────
// Transferência de ingresso
// ──────────────────────────────────────────────

type TransferRequest struct {
	CPF string `json:"cpf" binding:"required"`
}

// POST /tickets/:id/transfer
func TransferTicket(c *gin.Context) {
	userID, _ := c.Get("userID")
	ticketID := c.Param("id")

	var req TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CPF obrigatório"})
		return
	}

	cpf := sanitizeCPF(req.CPF)
	if len(cpf) != 11 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CPF inválido"})
		return
	}

	db := config.GetDB()

	// Verifica posse, status ativo e permissão de transferência do lote
	var allowTransfer bool
	err := db.QueryRow(`
		SELECT COALESCE(tb.allow_transfer, false)
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.id = $1
		  AND t.user_id = $2
		  AND t.status = 'valid'
		  AND o.status = 'paid'
	`, ticketID, userID).Scan(&allowTransfer)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não encontrado"})
		return
	}
	if err != nil {
		log.Printf("TransferTicket query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	if !allowTransfer {
		c.JSON(http.StatusForbidden, gin.H{"error": "este lote não permite transferência"})
		return
	}

	// Encontra o destinatário pelo CPF
	var recipientID string
	err = db.QueryRow(`
		SELECT id FROM users
		WHERE REPLACE(REPLACE(cpf, '.', ''), '-', '') = $1
	`, cpf).Scan(&recipientID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado com este CPF"})
		return
	}
	if err != nil {
		log.Printf("TransferTicket find user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	if recipientID == userID.(string) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "você não pode transferir para si mesmo"})
		return
	}

	// Bloqueia se estiver listado no Market
	var listed int
	db.QueryRow(`SELECT COUNT(*) FROM market_listings WHERE ticket_id = $1 AND status = 'active'`, ticketID).Scan(&listed)
	if listed > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "remova o ingresso do Reppy Market antes de transferir"})
		return
	}

	// Gera novo QR code para o destinatário — invalida o código anterior
	newQRCode, err := orderservice.GenerateQRCode()
	if err != nil {
		log.Printf("TransferTicket generate qr: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar QR code"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE tickets
		SET user_id = $1, status = 'valid', qr_code = $3, radar_enabled = false
		WHERE id = $2
	`, recipientID, ticketID, newQRCode)
	if err != nil {
		log.Printf("TransferTicket update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao transferir ingresso"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transferência"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ingresso transferido com sucesso"})
}

// ──────────────────────────────────────────────
// Reembolso
// ──────────────────────────────────────────────

type RefundRequest struct {
	Reason string `json:"reason"`
}

// POST /tickets/:id/refund
func RequestRefund(c *gin.Context) {
	userID, _ := c.Get("userID")
	ticketID := c.Param("id")

	var req RefundRequest
	c.ShouldBindJSON(&req)

	db := config.GetDB()

	var orderID string
	var ticketPrice float64
	err := db.QueryRow(`
		SELECT o.id, tb.price
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.id = $1
		  AND t.user_id = $2
		  AND t.status = 'valid'
		  AND o.status = 'paid'
	`, ticketID, userID).Scan(&orderID, &ticketPrice)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não encontrado ou não elegível para reembolso"})
		return
	}
	if err != nil {
		log.Printf("RequestRefund query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	// Evita duplicata de solicitação pendente
	var pendingCount int
	db.QueryRow(`SELECT COUNT(*) FROM refunds WHERE order_id = $1 AND status = 'pending'`, orderID).Scan(&pendingCount)
	if pendingCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "já existe uma solicitação de reembolso pendente para este ingresso"})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	refundID := uuid.New().String()
	_, err = tx.Exec(`
		INSERT INTO refunds (id, order_id, amount, reason, status, created_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
	`, refundID, orderID, ticketPrice, req.Reason, time.Now())
	if err != nil {
		log.Printf("RequestRefund insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao solicitar reembolso"})
		return
	}

	_, err = tx.Exec(`UPDATE tickets SET status = 'cancelled' WHERE id = $1`, ticketID)
	if err != nil {
		log.Printf("RequestRefund cancel ticket: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao cancelar ingresso"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar solicitação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":      refundID,
		"amount":  ticketPrice,
		"status":  "pending",
		"message": "solicitação de reembolso registrada",
	})
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

// activeMinPriceByCategory retorna o menor preço entre os lotes ativos
// do mesmo event_id e category_id. É o teto de referência para o Reppy Market.
// Quando category_id é NULL, usa o menor lote ativo do evento inteiro.
func activeMinPriceByCategory(db *sql.DB, eventID string, categoryID sql.NullString) (float64, error) {
	var minPrice sql.NullFloat64
	var err error

	if categoryID.Valid {
		err = db.QueryRow(`
			SELECT MIN(price)
			FROM ticket_batches
			WHERE event_id    = $1
			  AND category_id = $2
			  AND status      = 'active'
		`, eventID, categoryID.String).Scan(&minPrice)
	} else {
		err = db.QueryRow(`
			SELECT MIN(price)
			FROM ticket_batches
			WHERE event_id = $1
			  AND status   = 'active'
		`, eventID).Scan(&minPrice)
	}

	if err != nil || !minPrice.Valid {
		return 0, err
	}
	return minPrice.Float64, nil
}

func sanitizeCPF(cpf string) string {
	result := make([]byte, 0, 11)
	for i := 0; i < len(cpf); i++ {
		if cpf[i] >= '0' && cpf[i] <= '9' {
			result = append(result, cpf[i])
		}
	}
	return string(result)
}