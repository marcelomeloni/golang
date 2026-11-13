package webhooks

import (
	"bilheteria-api/handlers/webhook"
	"github.com/gin-gonic/gin"
)

func RegisterC6(r *gin.Engine) {
	// GET: health-check que o C6 faz para validar a URL antes de entregar notificações
	r.GET("/webhooks/c6", webhook.C6BankWebhookHealthCheck)
	// POST: recebe as notificações de Pix pagos
	r.POST("/webhooks/c6", webhook.C6BankWebhook)
}
