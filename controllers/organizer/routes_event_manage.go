package organizer

import (
	"github.com/gin-gonic/gin"
)

// RegisterEventManageRoutes registra todas as rotas de gestão de evento.
// Chamar dentro do grupo autenticado do router principal.
//
// Exemplo no main.go / router.go:
//
//	auth := r.Group("/org/:slug/events/:id", middleware.AuthMiddleware())
//	organizer.RegisterEventManageRoutes(auth)
func RegisterEventManageRoutes(rg *gin.RouterGroup) {
	// ── Overview (EventManageContext) ─────────────────────────────────────────
	// Qualquer membro da org
	rg.GET("/manage", GetEventManageHandler)

	// ── Check-in ─────────────────────────────────────────────────────────────
	// Qualquer membro (checkin_staff inclusive)
	rg.GET("/checkin-data", GetCheckinDataHandler)
	rg.PATCH("/checkin-data/:ticketID", PatchCheckinHandler)

	// ── Financeiro ────────────────────────────────────────────────────────────
	// Apenas owner/admin (ResolveOrgWithPermission internamente)
	rg.GET("/finance/painel", GetFinancePainelHandler)
	rg.GET("/finance/resumo", GetFinanceResumoHandler)

	// ── Cupons ────────────────────────────────────────────────────────────────
	rg.GET("/coupons", GetCouponsHandler)
	rg.POST("/coupons", CreateCouponHandler)
	rg.PATCH("/coupons/:couponID", PatchCouponHandler)
	rg.DELETE("/coupons/:couponID", DeleteCouponHandler)

	// ── Cancelamentos / Reembolsos ────────────────────────────────────────────
	rg.GET("/cancellations", GetCancellationsHandler)
	rg.PATCH("/cancellations/:refundID", PatchRefundHandler)

	// ── Participantes ─────────────────────────────────────────────────────────
	rg.GET("/participants", GetParticipantsHandler)
	rg.GET("/comunicados/recipients", GetComunicadosRecipientsHandler)
}