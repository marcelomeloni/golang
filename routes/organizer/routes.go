package organizer

import (
	controllers "bilheteria-api/controllers/organizer"
	"bilheteria-api/middleware"

	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	auth := r.Group("/", middleware.AuthMiddleware())
	{
		auth.GET("/auth/callback", controllers.AuthCallbackHandler)

		auth.POST("/onboarding", controllers.OnboardingHandler)

		auth.GET("/profile",   controllers.GetProfileHandler)
		auth.PATCH("/profile", controllers.UpdateProfileHandler)

		auth.GET("/dashboard", controllers.DashboardHandler)

		auth.GET("/org/:slug",    controllers.GetOrgHandler)
		auth.PATCH("/org/:slug",  controllers.UpdateOrgHandler)
		auth.DELETE("/org/:slug", controllers.DeleteOrgHandler)

		auth.POST("/org/:slug/logo",   controllers.UploadOrgLogoHandler)
		auth.POST("/org/:slug/banner", controllers.UploadOrgBannerHandler)

		auth.GET("/org/:slug/members",              controllers.GetMembersHandler)
		auth.POST("/org/:slug/members",             controllers.AddMemberHandler)
		auth.PATCH("/org/:slug/members/:memberID",  controllers.UpdateMemberRoleHandler)
		auth.DELETE("/org/:slug/members/:memberID", controllers.RemoveMemberHandler)

		auth.GET("/org/:slug/bank-accounts",                      controllers.GetBankAccountsHandler)
		auth.POST("/org/:slug/bank-accounts",                     controllers.AddBankAccountHandler)
		auth.PATCH("/org/:slug/bank-accounts/:accountID/default", controllers.SetDefaultBankAccountHandler)
		auth.DELETE("/org/:slug/bank-accounts/:accountID",        controllers.DeleteBankAccountHandler)

		auth.GET("/org/:slug/overview", controllers.GetOrgOverviewHandler)

		// ── Eventos ───────────────────────────────────────────────────────────
		auth.GET("/org/:slug/events",      controllers.GetOrgEventsHandler)
		auth.POST("/org/:slug/events",     controllers.SaveDraftHandler)

		auth.GET("/org/:slug/events/:id",           controllers.GetOrgEventDetailHandler)
		auth.PATCH("/org/:slug/events/:id",         controllers.UpdateEventHandler)
		auth.PATCH("/org/:slug/events/:id/publish", controllers.PublishEventHandler)
		auth.PATCH("/org/:slug/events/:id/cancel",  controllers.CancelEventHandler)
		auth.POST("/org/:slug/events/:id/banner",   controllers.UploadEventBannerHandler)

		// ── Manage (overview + sub-páginas) ───────────────────────────────────
		auth.GET("/org/:slug/events/:id/manage", controllers.GetEventManageHandler)

		auth.GET("/org/:slug/events/:id/checkin-data", controllers.GetCheckinDataHandler)
		auth.PATCH("/org/:slug/events/:id/checkin-data", controllers.PatchCheckinHandler)
		// Lookup por CPF quando o participante não tem o QR em mãos
		auth.GET("/org/:slug/events/:id/checkin-data/lookup", controllers.GetCheckinCPFLookupHandler)

		auth.GET("/org/:slug/events/:id/finance/painel", controllers.GetFinancePainelHandler)
		auth.GET("/org/:slug/events/:id/finance/resumo", controllers.GetFinanceResumoHandler)

		auth.GET("/org/:slug/events/:id/coupons",              controllers.GetCouponsHandler)
		auth.POST("/org/:slug/events/:id/coupons",             controllers.CreateCouponHandler)
		auth.PATCH("/org/:slug/events/:id/coupons/:couponID",  controllers.PatchCouponHandler)
		auth.DELETE("/org/:slug/events/:id/coupons/:couponID", controllers.DeleteCouponHandler)

		auth.GET("/org/:slug/events/:id/cancellations",              controllers.GetCancellationsHandler)
		auth.GET("/org/:slug/events/:id/cancellations/count",       controllers.GetRefundCountHandler)
		auth.PATCH("/org/:slug/events/:id/cancellations/:refundID",  controllers.PatchRefundHandler)
		auth.POST("/org/:slug/events/:id/comunicados",           controllers.SendComunicadoHandler)
		auth.GET("/org/:slug/events/:id/participants",              controllers.GetParticipantsHandler)
		auth.GET("/org/:slug/events/:id/comunicados/recipients",    controllers.GetComunicadosRecipientsHandler)

		// ── Lotes para emissão pelo promoter ────────────────────────────────────
		auth.GET("/org/:slug/events/:id/batches", controllers.GetEventBatchesHandler)

		// ── Promoter: emissão e histórico ───────────────────────────────────────
		// Promoter, admin, owner (gate de role dentro dos handlers)
		auth.POST("/org/:slug/events/:id/promoter/tickets/emit",         controllers.EmitPromoterTicketsHandler)
		auth.GET("/org/:slug/events/:id/promoter/tickets/distributions", controllers.GetPromoterDistributionsHandler)
		auth.GET("/org/:slug/events/:id/promoter/cpf-lookup",            controllers.GetPromoterCPFLookupHandler)
	}
}