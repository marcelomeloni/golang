package webhook

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

type abacateWebhookPayload struct {
	Event   string          `json:"event"`
	DevMode bool            `json:"devMode"`
	Data    json.RawMessage `json:"data"`
}

type abacateBillingData struct {
	PixQrCode struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Metadata struct {
			OrderID string `json:"order_id"`
		} `json:"metadata"`
	} `json:"pixQrCode"`
}

func AbacatePayWebhook(c *gin.Context) {
	secret := c.Query("webhookSecret")
	if secret != os.Getenv("ABACATEPAY_WEBHOOK_SECRET") {
		c.Status(http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("AbacatePayWebhook: erro ao ler body: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	// Log do payload completo para debug
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
		c.Status(http.StatusOK)
	}
}

func handleBillingPaid(c *gin.Context, db *sql.DB, raw json.RawMessage) {
	var data abacateBillingData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("handleBillingPaid: parse: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	log.Printf("handleBillingPaid: pixID=%s status=%s orderID=%s", data.PixQrCode.ID, data.PixQrCode.Status, data.PixQrCode.Metadata.OrderID)

	orderID := data.PixQrCode.Metadata.OrderID
	if orderID == "" {
		// fallback: busca pelo pix_external_id
		err := db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, data.PixQrCode.ID).Scan(&orderID)
		if err != nil {
			log.Printf("handleBillingPaid: fallback também falhou para id=%s: %v", data.PixQrCode.ID, err)
			c.Status(http.StatusOK)
			return
		}
		log.Printf("handleBillingPaid: encontrou via fallback pix_external_id orderID=%s", orderID)
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
		log.Printf("handleBillingPaid: update order: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		log.Printf("handleBillingPaid: orderID=%s já estava pago", orderID)
		c.Status(http.StatusOK)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("handleBillingPaid: commit: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("handleBillingPaid: orderID=%s confirmado ✓", orderID)
	c.Status(http.StatusOK)
}

func handleBillingRefunded(c *gin.Context, db *sql.DB, raw json.RawMessage) {
	var data abacateBillingData
	if err := json.Unmarshal(raw, &data); err != nil {
		log.Printf("handleBillingRefunded: parse: %v", err)
		c.Status(http.StatusBadRequest)
		return
	}

	orderID := data.PixQrCode.Metadata.OrderID
	if orderID == "" {
		_ = db.QueryRow(`SELECT id FROM orders WHERE pix_external_id = $1`, data.PixQrCode.ID).Scan(&orderID)
	}
	if orderID == "" {
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

	if _, err := tx.Exec(`UPDATE orders SET status = 'refunded', updated_at = NOW() WHERE id = $1`, orderID); err != nil {
		log.Printf("handleBillingRefunded: update order: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	if _, err := tx.Exec(`UPDATE tickets SET status = 'cancelled' WHERE order_id = $1`, orderID); err != nil {
		log.Printf("handleBillingRefunded: update tickets: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("handleBillingRefunded: commit: %v", err)
		c.Status(http.StatusInternalServerError)
		return
	}

	log.Printf("handleBillingRefunded: orderID=%s reembolsado ✓", orderID)
	c.Status(http.StatusOK)
}