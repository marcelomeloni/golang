package webhook

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"bilheteria-api/config"
	"bilheteria-api/services/orderservice"
	"bilheteria-api/services/paymentservice"
	"github.com/gin-gonic/gin"
)

type abacateWebhookPayload struct {
	Event   string          `json:"event"`
	DevMode bool            `json:"devMode"`
	Data    json.RawMessage `json:"data"`
}

type abacateBillingData struct {
	PixQrCode struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Metadata struct {
			OrderID string `json:"order_id"`
		} `json:"metadata"`
	} `json:"pixQrCode"`
}

func AbacatePayWebhook(c *gin.Context) {
	secret := c.Query("webhookSecret")
	if secret != os.Getenv("ABACATEPAY_WEBHOOK_SECRET") {
		log.Printf("AbacatePayWebhook: secret inválido — recebido=%q", secret)
		c.Status(http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("AbacatePayWebhook: erro ao ler body: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("AbacatePayWebhook RAW BODY: %s", string(body))

	var payload abacateWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("AbacatePayWebhook: parse error: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("AbacatePayWebhook: evento=%s devMode=%v data=%s", payload.Event, payload.DevMode, string(payload.Data))

	switch payload.Event {
	case "billing.paid":
		handleBillingPaid(c, config.GetDB(), payload.Data)
	case "billing.refunded", "checkout.refunded":
		// billing.refunded = estorno notificado pela API v1 (painel AbacatePay).
		// checkout.refunded = mesmo evento, mas notificado pela API v2.
		handleBillingRefunded(c, config.GetDB(), payload.Data)
	default:
		log.Printf("AbacatePayWebhook: evento desconhecido=%s — ignorando", payload.Event)
		c.Status(http.StatusOK)
	}
}

func handleBillingPaid(c *gin.Context, db *sql.DB, raw json.RawMessage) {
	log.Printf("handleBillingPaid: raw data=%s", string(raw))

	var data abacateBillingData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("handleBillingPaid: parse: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("handleBillingPaid: após parse — pixQrCode.ID=%q status=%q orderID=%q",
		data.PixQrCode.ID, data.PixQrCode.Status, data.PixQrCode.Metadata.OrderID)

	orderID := data.PixQrCode.Metadata.OrderID
	if orderID == "" {
		log.Printf("handleBillingPaid: metadata.order_id vazio — tentando fallback por pix_external_id=%q", data.PixQrCode.ID)

		if data.PixQrCode.ID == "" {
			log.Printf("handleBillingPaid: ATENÇÃO — pixQrCode.ID também vazio, struct pode estar deserializando errado")
			c.Status(http.StatusOK)
			return
		}

		err := db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, data.PixQrCode.ID).Scan(&orderID)
		if err != nil {
			log.Printf("handleBillingPaid: fallback falhou para pix_external_id=%q: %v", data.PixQrCode.ID, err)
			c.Status(http.StatusOK)
			return
		}
		log.Printf("handleBillingPaid: fallback encontrou orderID=%s", orderID)
	}

	// Pagamento de evento cancelado/encerrado: não emite ingresso — marca o
	// pedido como cancelado e devolve o dinheiro automaticamente.
	if cancelledEvent(db, orderID) {
		log.Printf("handleBillingPaid: orderID=%s pertence a evento não publicado — cancelando pedido e estornando", orderID)
		if cancelPaidOrderAndRefund(db, orderID, data.PixQrCode.ID, "Pagamento de evento cancelado") {
			c.Status(http.StatusOK)
			return
		}
		// Estorno não executado: segue o fluxo normal (order -> paid, ingressos
		// emitidos). O comprador solicita o reembolso pela via padrão.
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("handleBillingPaid: begin tx: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		UPDATE orders SET status = 'paid', updated_at = NOW()
		WHERE id = $1 AND status != 'paid'`, orderID)
	if err != nil {
		log.Printf("handleBillingPaid: update order orderID=%s: %v", orderID, err)
		c.Status(http.StatusInternalServerError)
		return
	}

	rows, _ := res.RowsAffected()
	log.Printf("handleBillingPaid: rows affected=%d orderID=%s", rows, orderID)

	if rows == 0 {
		log.Printf("handleBillingPaid: orderID=%s já estava pago ou não existe no banco", orderID)
		c.Status(http.StatusOK)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("handleBillingPaid: commit: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("handleBillingPaid: orderID=%s confirmado ✓", orderID)

	processMarketTransactionIfExists(db, orderID)

	c.Status(http.StatusOK)
}

// cancelledEvent diz se o pedido pertence a um evento que não está mais à venda.
func cancelledEvent(db *sql.DB, orderID string) bool {
	var status string
	err := db.QueryRow(`
		SELECT e.status
		FROM events e
		JOIN orders o ON o.event_id = e.id
		WHERE o.id = $1
	`, orderID).Scan(&status)
	if err != nil {
		// Sem evento associado (ou pedido inexistente): não bloqueia o pagamento.
		log.Printf("cancelledEvent: orderID=%s: %v", orderID, err)
		return false
	}
	return status != "published"
}

// cancelPaidOrderAndRefund estorna automaticamente um pedido pago de um evento
// que não está mais publicado, e só então cancela o pedido/ingressos.
//
// Retorna true quando o pedido foi cancelado. Se o estorno não puder ser
// executado (gateway indisponível, erro da API), devolve false e NÃO cancela: o
// fluxo normal de billing.paid segue (order -> paid, ingressos emitidos) e o
// comprador solicita o reembolso pela via padrão.
func cancelPaidOrderAndRefund(db *sql.DB, orderID, pixExternalID, reason string) bool {
	if pixExternalID == "" {
		_ = db.QueryRow(`SELECT pix_external_id FROM orders WHERE id = $1`, orderID).Scan(&pixExternalID)
	}
	if pixExternalID == "" {
		log.Printf("cancelPaidOrderAndRefund: orderID=%s sem pix_external_id — estorno manual necessário", orderID)
		return false
	}

	if paymentservice.Default == nil {
		log.Printf("cancelPaidOrderAndRefund: gateway não inicializado — estorno manual necessário, pedido será tratado como pago")
		return false
	}
	if err := paymentservice.Default.Refund(pixExternalID, reason); err != nil {
		log.Printf("cancelPaidOrderAndRefund: refund orderID=%s: %v (mantendo pedido como pago e emitindo ingressos)", orderID, err)
		return false
	}
	log.Printf("cancelPaidOrderAndRefund: orderID=%s estornado ✓ — cancelando pedido", orderID)

	tx, err := db.Begin()
	if err != nil {
		log.Printf("cancelPaidOrderAndRefund: begin orderID=%s: %v", orderID, err)
		return false
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE orders SET status = 'cancelled', updated_at = NOW() WHERE id = $1`, orderID); err != nil {
		log.Printf("cancelPaidOrderAndRefund: update order orderID=%s: %v", orderID, err)
		return false
	}
	if _, err := tx.Exec(`UPDATE tickets SET status = 'cancelled' WHERE order_id = $1`, orderID); err != nil {
		log.Printf("cancelPaidOrderAndRefund: cancel tickets orderID=%s: %v", orderID, err)
		return false
	}
	cancelMarketTransactionIfExists(tx, orderID)

	if err := tx.Commit(); err != nil {
		log.Printf("cancelPaidOrderAndRefund: commit orderID=%s: %v", orderID, err)
		return false
	}

	log.Printf("cancelPaidOrderAndRefund: orderID=%s cancelado após estorno ✓", orderID)
	return true
}

func handleBillingRefunded(c *gin.Context, db *sql.DB, raw json.RawMessage) {
	log.Printf("handleBillingRefunded: raw data=%s", string(raw))

	var data abacateBillingData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("handleBillingRefunded: parse: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("handleBillingRefunded: pixQrCode.ID=%q orderID=%q",
		data.PixQrCode.ID, data.PixQrCode.Metadata.OrderID)

	orderID := data.PixQrCode.Metadata.OrderID
	if orderID == "" {
		_ = db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, data.PixQrCode.ID).Scan(&orderID)
		log.Printf("handleBillingRefunded: fallback orderID=%q", orderID)
	}
	if orderID == "" {
		// Evento v2 (checkout.refunded) pode não trazer pixQrCode — tenta
		// localizar o pedido pelos IDs presentes no payload cru.
		orderID = findOrderIDInPayload(db, raw)
	}
	if orderID == "" {
		log.Printf("handleBillingRefunded: orderID não encontrado — abortando")
		c.Status(http.StatusOK)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("handleBillingRefunded: begin tx: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE orders SET status = 'refunded', updated_at = NOW() WHERE id = $1
	`, orderID); err != nil {
		log.Printf("handleBillingRefunded: update order: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`
		UPDATE tickets SET status = 'cancelled' WHERE order_id = $1
	`, orderID); err != nil {
		log.Printf("handleBillingRefunded: update tickets: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	// Fecha as solicitações de reembolso abertas deste pedido.
	if _, err := tx.Exec(`
		UPDATE refunds SET status = 'completed'
		WHERE order_id = $1 AND status IN ('pending', 'refunding')
	`, orderID); err != nil {
		log.Printf("handleBillingRefunded: update refunds: %v", err)
	}

	cancelMarketTransactionIfExists(tx, orderID)

	if err := tx.Commit(); err != nil {
		log.Printf("handleBillingRefunded: commit: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("handleBillingRefunded: orderID=%s reembolsado ✓", orderID)
	c.Status(http.StatusOK)
}

// findOrderIDInPayload varre o payload cru (webhook v2 tem formato diferente do
// v1) procurando o pedido por `order_id` ou pelo id da cobrança (`pix_char_...`).
func findOrderIDInPayload(db *sql.DB, raw json.RawMessage) string {
	var anyPayload any
	if err := json.Unmarshal(raw, &anyPayload); err != nil {
		return ""
	}

	var candidates []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			candidates = append(candidates, t)
		case []any:
			for _, item := range t {
				walk(item)
			}
		case map[string]any:
			for key, item := range t {
				if key == "order_id" {
					if s, ok := item.(string); ok && s != "" {
						candidates = append(candidates, s)
					}
				}
				walk(item)
			}
		}
	}
	walk(anyPayload)

	for _, cand := range candidates {
		var orderID string
		if err := db.QueryRow(`SELECT id FROM orders WHERE id = $1`, cand).Scan(&orderID); err == nil {
			return orderID
		}
		if err := db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, cand).Scan(&orderID); err == nil {
			return orderID
		}
	}
	return ""
}

// processMarketTransactionIfExists cria o ticket do comprador, transfere o ingresso
// e credita o saldo do vendedor — tudo após o pagamento ser confirmado.
//
// Operações em transação única:
//  1. Cria um novo ticket (valid) para o comprador
//  2. Invalida o ticket original do vendedor (transferred)
//  3. Fecha o listing (sold)
//  4. Registra o new_ticket_id, move escrow para 'held' e popula held_at
//  5. Credita market_balance do vendedor com o valor líquido
func processMarketTransactionIfExists(db *sql.DB, orderID string) {
	log.Printf("processMarketTransaction: iniciando para orderID=%s", orderID)

	var (
		txID        string
		listingID   string
		oldTicketID string
		buyerID     sql.NullString
		batchID     string
		sellerID    string
		amount      float64
		platformFee float64
	)

	err := db.QueryRow(`
		SELECT mt.id, mt.listing_id, ml.ticket_id, mt.buyer_id, t.batch_id,
		       ml.seller_id, mt.amount, mt.platform_fee
		FROM market_transactions mt
		JOIN market_listings ml ON ml.id = mt.listing_id
		JOIN tickets t ON t.id = ml.ticket_id
		WHERE mt.order_id = $1
		  AND mt.escrow_status = 'pending'
	`, orderID).Scan(&txID, &listingID, &oldTicketID, &buyerID, &batchID, &sellerID, &amount, &platformFee)

	if err == sql.ErrNoRows {
		log.Printf("processMarketTransaction: nenhuma market_transaction pending para orderID=%s — pedido normal, ignorando", orderID)
		return
	}
	if err != nil {
		log.Printf("processMarketTransaction: scan orderID=%s: %v", orderID, err)
		return
	}

	log.Printf("processMarketTransaction: txID=%s listingID=%s oldTicketID=%s buyerID=%v batchID=%s sellerID=%s",
		txID, listingID, oldTicketID, buyerID, batchID, sellerID)

	newQR, err := orderservice.GenerateQRCode()
	if err != nil {
		log.Printf("processMarketTransaction: generateQR orderID=%s: %v", orderID, err)
		return
	}

	sqlTx, err := db.Begin()
	if err != nil {
		log.Printf("processMarketTransaction: begin tx: %v", err)
		return
	}
	defer sqlTx.Rollback()

	var newTicketID string
	err = sqlTx.QueryRow(`
		INSERT INTO tickets (order_id, batch_id, user_id, qr_code, status)
		VALUES ($1, $2, $3, $4, 'valid')
		RETURNING id
	`, orderID, batchID, buyerID, newQR).Scan(&newTicketID)
	if err != nil {
		log.Printf("processMarketTransaction: insert ticket orderID=%s: %v", orderID, err)
		return
	}
	log.Printf("processMarketTransaction: novo ticket criado newTicketID=%s", newTicketID)

	type step struct {
		label string
		query string
		arg   string
	}

	steps := []step{
		{"invalidar ticket vendedor", `UPDATE tickets SET status = 'transferred' WHERE id = $1`, oldTicketID},
		{"fechar listing", `UPDATE market_listings SET status = 'sold', updated_at = NOW() WHERE id = $1`, listingID},
	}

	for _, s := range steps {
		if _, err := sqlTx.Exec(s.query, s.arg); err != nil {
			log.Printf("processMarketTransaction: step %q orderID=%s: %v", s.label, orderID, err)
			return
		}
		log.Printf("processMarketTransaction: step %q OK", s.label)
	}

	if _, err := sqlTx.Exec(`
		UPDATE market_transactions
		SET escrow_status = 'held', new_ticket_id = $1, held_at = NOW()
		WHERE id = $2
	`, newTicketID, txID); err != nil {
		log.Printf("processMarketTransaction: update escrow orderID=%s: %v", orderID, err)
		return
	}
	log.Printf("processMarketTransaction: escrow -> held txID=%s", txID)

	// Credita o saldo do vendedor com o valor líquido (após taxa da plataforma)
	netAmount := amount - platformFee
	if _, err := sqlTx.Exec(`
		UPDATE users
		SET market_balance = market_balance + $1
		WHERE id = $2
	`, netAmount, sellerID); err != nil {
		log.Printf("processMarketTransaction: update market_balance sellerID=%s: %v", sellerID, err)
		return
	}
	log.Printf("processMarketTransaction: market_balance += %.2f sellerID=%s ✓", netAmount, sellerID)

	if err := sqlTx.Commit(); err != nil {
		log.Printf("processMarketTransaction: commit orderID=%s: %v", orderID, err)
		return
	}

	log.Printf("processMarketTransaction: transferência concluída orderID=%s txID=%s newTicketID=%s ✓",
		orderID, txID, newTicketID)
}

// cancelMarketTransactionIfExists reabre o listing quando o pagamento é reembolsado.
// Deve ser chamado dentro de uma transação já aberta.
func cancelMarketTransactionIfExists(tx *sql.Tx, orderID string) {
	var listingID string

	err := tx.QueryRow(`
		SELECT listing_id FROM market_transactions
		WHERE order_id = $1 AND escrow_status IN ('pending', 'held')
	`, orderID).Scan(&listingID)

	if err == sql.ErrNoRows {
		log.Printf("cancelMarketTransaction: nenhuma transaction ativa para orderID=%s", orderID)
		return
	}
	if err != nil {
		log.Printf("cancelMarketTransaction: scan orderID=%s: %v", orderID, err)
		return
	}

	if _, err := tx.Exec(`
		UPDATE market_listings SET status = 'active', updated_at = NOW() WHERE id = $1
	`, listingID); err != nil {
		log.Printf("cancelMarketTransaction: reopen listing orderID=%s: %v", orderID, err)
		return
	}

	if _, err := tx.Exec(`
		UPDATE market_transactions SET escrow_status = 'cancelled' WHERE order_id = $1
	`, orderID); err != nil {
		log.Printf("cancelMarketTransaction: update escrow orderID=%s: %v", orderID, err)
		return
	}

	log.Printf("cancelMarketTransaction: listing reaberto orderID=%s listingID=%s ✓", orderID, listingID)
}