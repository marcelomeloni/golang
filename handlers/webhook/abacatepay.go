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
	case "billing.refunded":
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

	cancelMarketTransactionIfExists(tx, orderID)

	if err := tx.Commit(); err != nil {
		log.Printf("handleBillingRefunded: commit: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("handleBillingRefunded: orderID=%s reembolsado ✓", orderID)
	c.Status(http.StatusOK)
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