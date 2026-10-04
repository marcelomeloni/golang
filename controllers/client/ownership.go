package client

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// requireSelf resolve o dono do recurso a partir do token (claim "sub" do JWT,
// validado pelo JWKS em middleware.AuthMiddleware) e garante que ele coincide
// com o :userId da URL.
//
// O ID do token é a única fonte de verdade: quando o parametro existe e
// diverge, a requisicao e recusada com 403 em vez de operar em nome de outro
// usuario. Quando o parametro nao existe (ex.: POST /auth/complete-profile), o
// ID do token e usado diretamente.
//
// Deve ser chamada por handlers registrados dentro do grupo que usa
// middleware.AuthMiddleware(). Fora desse grupo o contexto nao tem "userID" e a
// funcao devolve 401.
func requireSelf(c *gin.Context) (string, bool) {
	tokenUserID, _ := c.Get("userID")
	userID, _ := tokenUserID.(string)
	userID = strings.TrimSpace(userID)

	if userID == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "token ausente ou invalido",
		})
		return "", false
	}

	if param := strings.TrimSpace(c.Param("userId")); param != "" && param != userID {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": "voce nao pode acessar dados de outro usuario",
		})
		return "", false
	}

	return userID, true
}