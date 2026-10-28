package webhook

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/orderservice"
	"github.com/gin-gonic/gin"
)

type c6WebhookPix struct {
	EndToEndId string `json:"endToEndId"`
	Txid       string `json:"txid"`
	Valor      string `json:"valor"`
	Horario    string `json:"horario"`
	Chave      string `json:"chave"`
	InfoPagador string `json:"infoPagador"`
}

type c6WebhookPayload struct {
	Pix []c6WebhookPix `json:"pix"`
}

func C6BankWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("C6BankWebhook: erro ao ler body: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("C6BankWebhook RAW BODY: %s", string(body))

	var payload c6WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("C6BankWebhook: parse error: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	if len(payload.Pix) == 0 {
		log.Printf("C6BankWebhook: payload sem pix[] — ignorando")
		c.Status(http.StatusOK)
		return
	}

	db := config.GetDB()

	for _, pix := range payload.Pix {
		if pix.Txid == "" {
			log.Printf("C6BankWebhook: item sem txid — ignorando")
			continue
		}

		var orderID string
		err := db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, pix.Txid).Scan(&orderID)
		if err != nil {
			if err == sql.ErrNoRows {
				log.Printf("C6BankWebhook: pedido não encontrado para txid=%s", pix.Txid)
				continue
			}
			log.Printf("C6BankWebhook: erro ao buscar pedido por txid=%s: %v", pix.Txid, err)
			continue
		}

		log.Printf("C6BankWebhook: txid=%s -> orderID=%s recebido", pix.Txid, orderID)

		// Pagamento de evento cancelado/encerrado: não emite ingresso — marca o
		// pedido como cancelado e devolve o dinheiro automaticamente.
		if cancelledEvent(db, orderID) {
			log.Printf("C6BankWebhook: orderID=%s pertence a evento não publicado — cancelando pedido e estornando", orderID)
			if cancelPaidOrderAndRefund(db, orderID, pix.Txid, "Pagamento de evento cancelado") {
				continue
			}
			// Estorno não executado: segue o fluxo normal
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("C6BankWebhook: begin tx orderID=%s: %v", orderID, err)
			continue
		}

		res, err := tx.Exec(`
			UPDATE orders SET status = 'paid', updated_at = NOW()
			WHERE id = $1 AND status != 'paid'`, orderID)
		if err != nil {
			log.Printf("C6BankWebhook: update order orderID=%s: %v", orderID, err)
			tx.Rollback()
			continue
		}

		rows, _ := res.RowsAffected()
		if rows == 0 {
			log.Printf("C6BankWebhook: orderID=%s já estava pago ou não existe", orderID)
			tx.Rollback()
			continue
		}

		if err := tx.Commit(); err != nil {
			log.Printf("C6BankWebhook: commit orderID=%s: %v", orderID, err)
			continue
		}

		log.Printf("C6BankWebhook: orderID=%s confirmado ✓", orderID)

		processMarketTransactionIfExists(db, orderID)

		go func(oid string) {
			if err := orderservice.SendConfirmationEmail(db, emailsender.New(""), oid); err != nil {
				log.Printf("C6BankWebhook SendConfirmationEmail orderID=%s: %v", oid, err)
			}
		}(orderID)
	}

	c.Status(http.StatusOK)
}
