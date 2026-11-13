package admin

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bilheteria-api/services/paymentservice"
	"github.com/gin-gonic/gin"
)

func getWebhookManager(c *gin.Context) (paymentservice.WebhookManager, bool) {
	wm, ok := paymentservice.DefaultWebhookManager()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "gateway C6 Bank não está configurado"})
		return nil, false
	}
	return wm, true
}

func respondC6Error(c *gin.Context, err error) {
	var apiErr *paymentservice.C6APIError
	if errors.As(err, &apiErr) {
		log.Printf("C6 webhook error status=%d correlationId=%s: %s", apiErr.Status, apiErr.CorrelationID, apiErr.Error())
		c.JSON(http.StatusBadGateway, gin.H{
			"error": apiErr.Error(),
			"c6": gin.H{
				"status":         apiErr.Status,
				"title":          apiErr.Title,
				"type":           apiErr.Type,
				"detail":         apiErr.Detail,
				"correlation_id": apiErr.CorrelationID,
				"violacoes":      apiErr.Violacoes,
			},
		})
		return
	}

	log.Printf("C6 webhook error: %v", err)
	c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
}

func isLocalWebhookHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()
}

// GetC6Webhook devolve o webhook cadastrado para a chave configurada no
// gateway. Se o C6 responder 404, a resposta indica configured=false.
func GetC6Webhook(c *gin.Context) {
	wm, ok := getWebhookManager(c)
	if !ok {
		return
	}

	chave := wm.PixKey()
	info, err := wm.GetWebhook(chave)
	if err != nil {
		if errors.Is(err, paymentservice.ErrWebhookNotFound) {
			c.JSON(http.StatusOK, gin.H{
				"environment": wm.Environment(),
				"pix_key":     chave,
				"configured":  false,
				"webhook":     nil,
			})
			return
		}
		respondC6Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"environment": wm.Environment(),
		"pix_key":     chave,
		"configured":  true,
		"webhook":     info,
	})
}

type setC6WebhookRequest struct {
	WebhookURL string `json:"webhook_url"`
}

// SetC6Webhook cria ou atualiza o webhook da chave configurada.
func SetC6Webhook(c *gin.Context) {
	wm, ok := getWebhookManager(c)
	if !ok {
		return
	}

	var req setC6WebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "corpo inválido"})
		return
	}

	rawURL := strings.TrimSpace(req.WebhookURL)
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe a URL do webhook"})
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "URL do webhook inválida"})
		return
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a URL do webhook deve usar http ou https"})
		return
	}
	if wm.Environment() == "production" {
		if parsed.Scheme != "https" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "em produção o webhook precisa usar https"})
			return
		}
		if isLocalWebhookHost(parsed.Hostname()) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "em produção o webhook não pode apontar para localhost ou IP privado"})
			return
		}
	}

	info, err := wm.SetWebhook(wm.PixKey(), rawURL)
	if err != nil {
		respondC6Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":     "webhook configurado",
		"environment": wm.Environment(),
		"webhook":     info,
	})
}

// DeleteC6Webhook remove o webhook da chave configurada.
func DeleteC6Webhook(c *gin.Context) {
	wm, ok := getWebhookManager(c)
	if !ok {
		return
	}

	if err := wm.DeleteWebhook(wm.PixKey()); err != nil {
		if errors.Is(err, paymentservice.ErrWebhookNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
		respondC6Error(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// ListC6Webhooks consulta os webhooks cadastrados no C6.
func ListC6Webhooks(c *gin.Context) {
	wm, ok := getWebhookManager(c)
	if !ok {
		return
	}

	params := paymentservice.WebhookListParams{
		PaginaAtual:    0,
		ItensPorPagina: 100,
	}

	if v := strings.TrimSpace(c.Query("inicio")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro inicio inválido; use RFC3339"})
			return
		}
		params.Inicio = &t
	}
	if v := strings.TrimSpace(c.Query("fim")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro fim inválido; use RFC3339"})
			return
		}
		params.Fim = &t
	}
	if v := strings.TrimSpace(c.Query("pagina")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro pagina inválido"})
			return
		}
		params.PaginaAtual = n
	}
	if v := strings.TrimSpace(c.Query("itens")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "parâmetro itens inválido"})
			return
		}
		if n > 1000 {
			n = 1000
		}
		params.ItensPorPagina = n
	}

	result, err := wm.ListWebhooks(params)
	if err != nil {
		respondC6Error(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"environment": wm.Environment(),
		"pix_key":     wm.PixKey(),
		"webhooks":    result.Webhooks,
		"parametros":  result.Parametros,
	})
}
