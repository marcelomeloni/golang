package client

import (
	"bilheteria-api/controllers/client"
	"bilheteria-api/middleware"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	clientGroup := r.Group("/client")
	{
		// Auth
		clientGroup.GET("/auth/profile/:userId",   client.CheckProfile)
		clientGroup.POST("/auth/complete-profile", client.CompleteProfile)

		// Usuário
		clientGroup.GET("/users/:userId",         client.GetUserProfile)
		clientGroup.PATCH("/users/:userId",       client.UpdateUserProfile)
		clientGroup.POST("/users/:userId/avatar", client.UploadUserAvatar)

		// Eventos
		clientGroup.GET("/home-events",  client.GetHomeEvents)
		clientGroup.GET("/events/:slug", client.GetEventDetail)
		clientGroup.GET("/search",       client.Search)

		// Organizadores (OptionalAuth para saber se o usuário logado já segue)
		clientGroup.GET("/organizers/:slug", middleware.OptionalAuth(), client.GetOrganizerDetail)

		// Checkout — sem auth obrigatória (suporta guests)
		clientGroup.POST("/checkout/coupon",                       middleware.OptionalAuth(), client.ValidateCoupon)
		clientGroup.POST("/checkout/orders",                       middleware.OptionalAuth(), client.CreateOrder)
		clientGroup.GET("/checkout/orders/:orderID/pix/status",   middleware.OptionalAuth(), client.CheckPixStatus)

		// Rotas autenticadas — JWT obrigatório
		authed := clientGroup.Group("/", middleware.AuthMiddleware())
		{
			// Meus ingressos
			authed.GET("/my-tickets",              client.GetMyTickets)
			authed.GET("/my-tickets/:id/download", client.DownloadTicket)

			// Organizadores - Seguir/Deixar de Seguir
			authed.POST("/organizers/:slug/follow",   client.FollowOrganizer)
			authed.DELETE("/organizers/:slug/follow", client.UnfollowOrganizer)

			// Ações sobre ingresso
			authed.POST("/tickets/:id/transfer", client.TransferTicket)
			authed.POST("/tickets/:id/refund",   client.RequestRefund)

			// Reppy Market
			authed.POST("/tickets/:id/market",           client.CreateMarketListing)
			authed.PATCH("/market/listings/:listingId",  client.UpdateMarketListing)
			authed.DELETE("/market/listings/:listingId", client.DeleteMarketListing)

			// Reppy Radar — prefixo /radar/events para evitar conflito com /events/:slug
			authed.GET("/radar/events/:eventId",                      client.GetRadarProfiles)
			authed.GET("/radar/events/:eventId/mode",                 client.GetRadarMode)
			authed.PATCH("/radar/events/:eventId/mode",               client.ToggleRadarMode)
			authed.POST("/radar/events/:eventId/tap/:targetUserId",   client.TapUser)
			authed.DELETE("/radar/events/:eventId/tap/:targetUserId", client.RemoveTap)
			authed.POST("/radar/block/:targetUserId",                 client.BlockRadarUser)
			authed.DELETE("/radar/block/:targetUserId",               client.UnblockRadarUser)
		}
	}
}