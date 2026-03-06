// routes/client/routes.go
package client

import (
	"bilheteria-api/controllers/client"
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	clientGroup := r.Group("/client")
	{
		// Auth
		clientGroup.GET("/auth/profile/:userId",   client.CheckProfile)
		clientGroup.POST("/auth/complete-profile", client.CompleteProfile)

		// Usuário
		clientGroup.GET("/users/:userId",   client.GetUserProfile)
		clientGroup.PATCH("/users/:userId", client.UpdateUserProfile)
		clientGroup.POST("/users/:userId/avatar", client.UploadUserAvatar)
		// Eventos
		clientGroup.GET("/home-events",  client.GetHomeEvents)
		clientGroup.GET("/events/:slug", client.GetEventDetail)

		// Organizadores
		clientGroup.GET("/organizers/:slug",           client.GetOrganizerDetail)
		clientGroup.POST("/organizers/:slug/follow",   client.FollowOrganizer)
		clientGroup.DELETE("/organizers/:slug/follow", client.UnfollowOrganizer)
	}
}