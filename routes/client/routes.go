package client

import (
	"bilheteria-api/controllers/client"
	"bilheteria-api/middleware"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	clientGroup := r.Group("/client")
	{
		clientGroup.GET("/auth/profile/:userId",   client.CheckProfile)
		clientGroup.POST("/auth/complete-profile", client.CompleteProfile)

		clientGroup.GET("/users/:userId",         client.GetUserProfile)
		clientGroup.PATCH("/users/:userId",       client.UpdateUserProfile)
		clientGroup.POST("/users/:userId/avatar", client.UploadUserAvatar)

		clientGroup.GET("/home-events",  client.GetHomeEvents)
		clientGroup.GET("/events/:slug", client.GetEventDetail)
		clientGroup.GET("/search",       client.Search)

		clientGroup.GET("/organizers/:slug", middleware.OptionalAuth(), client.GetOrganizerDetail)

		clientGroup.POST("/checkout/coupon",                            middleware.OptionalAuth(), client.ValidateCoupon)
		clientGroup.POST("/checkout/orders",                            middleware.OptionalAuth(), client.CreateOrder)
		clientGroup.GET("/checkout/orders/:orderID/pix/status",        middleware.OptionalAuth(), client.CheckPixStatus)
		clientGroup.POST("/checkout/market",                            middleware.OptionalAuth(), client.CreateMarketOrder)
		clientGroup.GET("/checkout/market/:transactionId/pix/status",  middleware.OptionalAuth(), client.CheckMarketPixStatus)

		authed := clientGroup.Group("/", middleware.AuthMiddleware())
		{
			authed.GET("/my-tickets",              client.GetMyTickets)
			authed.GET("/my-tickets/:id/download", client.DownloadTicket)

			authed.POST("/organizers/:slug/follow",   client.FollowOrganizer)
			authed.DELETE("/organizers/:slug/follow", client.UnfollowOrganizer)

			authed.POST("/tickets/:id/transfer", client.TransferTicket)
			authed.POST("/tickets/:id/refund",   client.RequestRefund)

			authed.POST("/tickets/:id/market",           client.CreateMarketListing)
			authed.PATCH("/market/listings/:listingId",  client.UpdateMarketListing)
			authed.DELETE("/market/listings/:listingId", client.DeleteMarketListing)

			authed.GET("/radar/events/:eventId",                      client.GetRadarProfiles)
			authed.GET("/radar/events/:eventId/mode",                 client.GetRadarMode)
			authed.PATCH("/radar/events/:eventId/mode",               client.ToggleRadarMode)
			authed.POST("/radar/events/:eventId/tap/:targetUserId",   client.TapUser)
			authed.DELETE("/radar/events/:eventId/tap/:targetUserId", client.RemoveTap)

			authed.GET("/radar/blocks",                  client.GetBlockedUsers)
			authed.POST("/radar/block/:targetUserId",    client.BlockRadarUser)
			authed.DELETE("/radar/block/:targetUserId",  client.UnblockRadarUser)
		}
	}
}