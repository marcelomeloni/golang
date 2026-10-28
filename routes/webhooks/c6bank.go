package webhooks

import (
	"bilheteria-api/handlers/webhook"
	"github.com/gin-gonic/gin"
)

func RegisterC6(r *gin.Engine) {
	r.POST("/webhooks/c6", webhook.C6BankWebhook)
}
