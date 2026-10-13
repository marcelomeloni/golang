package middleware

import (
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// AdminMiddleware exige que o usuário autenticado (claim "sub" já setado pelo
// AuthMiddleware) seja um admin da plataforma (tabela platform_admins).
// Deve ser usado SEMPRE depois do AuthMiddleware.
func AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := c.Get("userID")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
			return
		}

		uid, ok := userID.(string)
		if !ok || uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "usuário inválido"})
			return
		}

		db := config.GetDB()

		var exists bool
		err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM platform_admins WHERE user_id = $1)`, uid,
		).Scan(&exists)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "erro ao verificar permissão"})
			return
		}
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "acesso restrito a administradores da plataforma"})
			return
		}

		c.Set("isPlatformAdmin", true)
		c.Next()
	}
}
