package organizer

import (
	controllers "bilheteria-api/controllers/organizer"
	"bilheteria-api/middleware"

	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	auth := r.Group("/", middleware.AuthMiddleware())
	{
		// Auth
		auth.GET("/auth/callback", controllers.AuthCallbackHandler)

		// Onboarding
		auth.POST("/onboarding", controllers.OnboardingHandler)

		// Perfil do usuário
		auth.GET("/profile", controllers.GetProfileHandler)
		auth.PATCH("/profile", controllers.UpdateProfileHandler)

		// Dashboard
		auth.GET("/dashboard", controllers.DashboardHandler)

		// Organização
		auth.GET("/org/:slug",   controllers.GetOrgHandler)
		auth.PATCH("/org/:slug", controllers.UpdateOrgHandler)
		auth.DELETE("/org/:slug", controllers.DeleteOrgHandler)
		// Equipe
		auth.GET("/org/:slug/members",              controllers.GetMembersHandler)
		auth.POST("/org/:slug/members",             controllers.AddMemberHandler)
		auth.PATCH("/org/:slug/members/:memberID",  controllers.UpdateMemberRoleHandler)
		auth.DELETE("/org/:slug/members/:memberID", controllers.RemoveMemberHandler)

		// Contas bancárias
		auth.GET("/org/:slug/bank-accounts",                      controllers.GetBankAccountsHandler)
		auth.POST("/org/:slug/bank-accounts",                     controllers.AddBankAccountHandler)
		auth.PATCH("/org/:slug/bank-accounts/:accountID/default", controllers.SetDefaultBankAccountHandler)
		auth.DELETE("/org/:slug/bank-accounts/:accountID",        controllers.DeleteBankAccountHandler)

		auth.GET("/org/:slug/overview", controllers.GetOrgOverviewHandler)

// Eventos da org
auth.GET("/org/:slug/events",           controllers.GetOrgEventsHandler)
auth.GET("/org/:slug/events/:eventID",  controllers.GetOrgEventDetailHandler)
	}
}