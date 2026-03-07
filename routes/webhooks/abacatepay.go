package webhooks

import (
	"bilheteria-api/handlers/webhook"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	r.POST("/webhooks/abacatepay", webhook.AbacatePayWebhook)
}