package admin

import (
	controllers "bilheteria-api/controllers/admin"
	"bilheteria-api/middleware"

	"github.com/gin-gonic/gin"
)

// Register monta o grupo /admin. O login é público; todas as demais rotas
// exigem token válido (AuthMiddleware) E vínculo em platform_admins
// (AdminMiddleware).
func Register(r *gin.Engine) {
	adminGroup := r.Group("/admin")

	// Público
	adminGroup.POST("/auth/login", controllers.AdminLogin)

	// Protegido
	authed := adminGroup.Group("/", middleware.AuthMiddleware(), middleware.AdminMiddleware())
	{
		authed.GET("/auth/me", controllers.AdminMe)

		authed.GET("/overview", controllers.GetOverview)

		authed.GET("/orgs", controllers.ListOrgs)
		authed.GET("/orgs/:id", controllers.GetOrg)
		authed.GET("/orgs/:id/events", controllers.ListOrgEvents)
		authed.PATCH("/orgs/:id/fee", controllers.UpdateOrgFee)

		authed.GET("/events/:id", controllers.EventDetail)

		authed.GET("/payouts", controllers.ListPayouts)
		authed.POST("/payouts/:id/pay", controllers.PayPayout)
		authed.POST("/payouts/:id/reject", controllers.RejectPayout)

		authed.GET("/market-withdraws", controllers.ListMarketWithdraws)
		authed.POST("/market-withdraws/:id/pay", controllers.PayMarketWithdraw)
		authed.POST("/market-withdraws/:id/reject", controllers.RejectMarketWithdraw)

		authed.GET("/c6/webhook", controllers.GetC6Webhook)
		authed.PUT("/c6/webhook", controllers.SetC6Webhook)
		authed.DELETE("/c6/webhook", controllers.DeleteC6Webhook)
		authed.GET("/c6/webhooks", controllers.ListC6Webhooks)
	}
}
