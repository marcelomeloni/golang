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

	// Bloqueia ingresso gratuito — não faz sentido revender
	if req.Price <= 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "ingressos gratuitos não podem ser vendidos no Reppy Market"})
		return
	}

	// Regra central: preço deve ser menor que o maior lote ativo da mesma categoria.
	currentMax, err := activeMaxPriceByCategory(db, eventID, categoryID)
	if err != nil || currentMax == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "nenhum lote ativo encontrado para esta categoria"})
		return
	}

	if req.Price >= currentMax {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "preço deve ser menor que o lote atual da categoria",
			"maxPrice": currentMax - 0.01,
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

	// Mesma regra: preço < maior lote ativo da categoria
	currentMax, err := activeMaxPriceByCategory(db, eventID, categoryID)
	if err != nil || currentMax == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "nenhum lote ativo encontrado para esta categoria"})
		return
	}

	if req.Price >= currentMax {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":    "preço deve ser menor que o lote atual da categoria",
			"maxPrice": currentMax - 0.01,
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
		UPDATE market_listings SET status = 'canceled', updated_at = NOW()
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

// transferResult carrega o que a notificação por e-mail precisa saber.
// É montada depois do commit e consumida por notifyTicketTransfer.
type transferResult struct {
	RecipientID    string
	RecipientName  string
	RecipientEmail string
	RecipientGuest bool
	SenderCPF      string
	EventID        string
	EventName      string
	TicketID       string
	LoteName       string
	NewQRCode      string
}

// POST /tickets/:id/transfer
//
// Transfere a posse do ingresso para outro usuário, localizado pelo CPF.
//
// Invariantes garantidas aqui:
//   - só o dono atual de um ingresso válido e pago pode transferir;
//   - o lote precisa ter allow_transfer;
//   - não pode transferir para si mesmo;
//   - não pode transferir enquanto o ingresso estiver anunciado no Market;
//   - o QR anterior morre (novo QR é gerado) — o QR antigo não dá mais entrada;
//   - radar_enabled é desligado, porque visibilidade no radar é da pessoa, não do ingresso;
//   - radar_taps do remetente para este evento são limpos (órfãos sem dono);
//   - a transferência fica registrada em ticket_transfers.
//
// Todas as checagens rodam DENTRO da transação e o UPDATE final é
// condicional a t.user_id = remetente. Era o que faltava: a versão anterior
// validava fora do tx, então duas requisições simultâneas passavam pelas duas
// checagens e a segunda sobrescrevia a primeira.
func TransferTicket(c *gin.Context) {
	userID, ok := c.Get("userID")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}
	senderID, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}
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
	if !validarCPF(cpf) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CPF inválido"})
		return
	}

	tx, err := config.GetDB().Begin()
	if err != nil {
		log.Printf("TransferTicket begin: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	defer tx.Rollback()

	// 1. Posse + status + permissão do lote, já travando a linha do ticket.
	//    FOR UPDATE serializa transferências concorrentes do mesmo ingresso.
	var (
		allowTransfer bool
		eventID       string
		eventName     string
		previousQR    sql.NullString
		loteName      sql.NullString
	)
	err = tx.QueryRow(`
		SELECT
			COALESCE(tb.allow_transfer, false),
			e.id,
			e.title,
			t.qr_code,
			tb.name
		FROM tickets t
		JOIN orders o ON o.id = t.order_id
		JOIN events e ON e.id = o.event_id
		LEFT JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.id = $1
		  AND t.user_id = $2
		  AND t.status = 'valid'
		  AND o.status = 'paid'
		FOR UPDATE OF t
	`, ticketID, senderID).Scan(&allowTransfer, &eventID, &eventName, &previousQR, &loteName)
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

	// 2. Destinatário. Usa cpf_digits (coluna gerada, indexada) em vez de
	//    REPLACE(REPLACE(cpf,...)) sobre a coluna crua — mesma semântica,
	//    mas sem varrer a tabela a cada transferência.
	var (
		recipientID    string
		recipientName  sql.NullString
		recipientEmail sql.NullString
		recipientGuest bool
	)
	err = tx.QueryRow(`
		SELECT id, full_name, email, COALESCE(is_guest, false)
		FROM users
		WHERE cpf_digits = $1
	`, cpf).Scan(&recipientID, &recipientName, &recipientEmail, &recipientGuest)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "usuário não encontrado com este CPF",
			"code":    "recipient_not_found",
		})
		return
	}
	if err != nil {
		log.Printf("TransferTicket find user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}

	if recipientID == senderID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "você não pode transferir para si mesmo"})
		return
	}

	// 3. Market. O erro precisa ser checado: engolido, ele deixaria
	//    listed = 0 e a transferência passaria com o ingresso anunciado.
	var listed int
	if err := tx.QueryRow(
		`SELECT COUNT(*) FROM market_listings WHERE ticket_id = $1 AND status = 'active'`,
		ticketID,
	).Scan(&listed); err != nil {
		log.Printf("TransferTicket market check: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	}
	if listed > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "remova o ingresso do Reppy Market antes de transferir"})
		return
	}

	// 4. Novo QR invalida o anterior — o QR do remetente deixa de valer.
	newQRCode, err := orderservice.GenerateQRCode()
	if err != nil {
		log.Printf("TransferTicket generate qr: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar QR code"})
		return
	}

	var senderCPF sql.NullString
	_ = tx.QueryRow(`SELECT cpf FROM users WHERE id = $1`, senderID).Scan(&senderCPF)

	// 5. Transferência. O WHERE repete user_id e status: é a rede de segurança
	//    final contra corrida, mesmo com o FOR UPDATE acima.
	res, err := tx.Exec(`
		UPDATE tickets
		SET user_id = $1, status = 'valid', qr_code = $3, radar_enabled = false
		WHERE id = $2
		  AND user_id = $4
		  AND status = 'valid'
	`, recipientID, ticketID, newQRCode, senderID)
	if err != nil {
		log.Printf("TransferTicket update: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao transferir ingresso"})
		return
	}
	if affected, err := res.RowsAffected(); err != nil {
		log.Printf("TransferTicket rows affected: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro interno"})
		return
	} else if affected == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "ingresso não está mais disponível para transferência"})
		return
	}

	// 6. Radar: com radar_enabled = false o remetente sai das listas de todo
	//    mundo, mas os radar_taps que o mencionam continuariam no banco sem
	//    alcance. A lista de perfis do radar é puxada a partir de tickets
	//    (reppy_radar_controller.go), então esses toques viram linhas morto.
	//    Apaga nos dois sentidos porque quem tinha tocado no remetente
	//    também ficaria apontando para alguém que não aparece mais.
	if _, err := tx.Exec(`
		DELETE FROM radar_taps
		WHERE event_id = $2
		  AND (from_user_id = $1 OR to_user_id = $1)
	`, senderID, eventID); err != nil {
		log.Printf("TransferTicket cleanup radar_taps: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao transferir ingresso"})
		return
	}

	// 7. Auditoria. Sem histórico, "meu ingresso sumiu" não tem resposta.
	if _, err := tx.Exec(`
		INSERT INTO ticket_transfers (
			ticket_id, from_user_id, to_user_id, from_cpf, to_cpf,
			previous_qr_code, new_qr_code, event_id, from_is_guest, to_is_guest
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
			COALESCE((SELECT is_guest FROM users WHERE id = $2), false),
			$9
		)
	`, ticketID, senderID, recipientID, senderCPF, cpf,
		previousQR, newQRCode, eventID, recipientGuest,
	); err != nil {
		log.Printf("TransferTicket audit insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao registrar transferência"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("TransferTicket commit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transferência"})
		return
	}

	result := transferResult{
		RecipientID:    recipientID,
		RecipientName:  recipientName.String,
		RecipientEmail: recipientEmail.String,
		RecipientGuest: recipientGuest,
		SenderCPF:      senderCPF.String,
		EventID:        eventID,
		EventName:      eventName,
		TicketID:       ticketID,
		LoteName:       loteName.String,
		NewQRCode:      newQRCode,
	}

	// 8. Notificação fora da transação: e-mail é I/O lento e não pode
	//    segurar o lock do ticket. Se falhar, a transferência já está
	//    confirmada no banco — o usuário ainda vê o erro do e-mail no log,
	//    mas o ingresso está transferido. Pior caso: destinatário não
	//    avisado, o que a tela de "meus ingressos" ainda compensa.
	go notifyTicketTransfer(result)

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

// activeMaxPriceByCategory retorna o maior preço entre os lotes ativos
// do mesmo event_id e category_id. É o teto de referência para o Reppy Market:
// o vendedor não pode anunciar acima do lote mais caro em circulação.
// Quando category_id é NULL, usa o maior lote ativo do evento inteiro.
func activeMaxPriceByCategory(db *sql.DB, eventID string, categoryID sql.NullString) (float64, error) {
	var maxPrice sql.NullFloat64
	var err error

	if categoryID.Valid {
		err = db.QueryRow(`
			SELECT MAX(price)
			FROM ticket_batches
			WHERE event_id    = $1
			  AND category_id = $2
			  AND status      = 'active'
		`, eventID, categoryID.String).Scan(&maxPrice)
	} else {
		err = db.QueryRow(`
			SELECT MAX(price)
			FROM ticket_batches
			WHERE event_id = $1
			  AND status   = 'active'
		`, eventID).Scan(&maxPrice)
	}

	if err != nil || !maxPrice.Valid {
		return 0, err
	}
	return maxPrice.Float64, nil
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