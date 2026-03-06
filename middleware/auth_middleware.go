package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

var jwks keyfunc.Keyfunc

func InitJWKS() {
	supabaseURL := os.Getenv("NEXT_PUBLIC_SUPABASE_URL")
	if supabaseURL == "" {
		supabaseURL = os.Getenv("SUPABASE_URL")
	}

	jwksURL := supabaseURL + "/auth/v1/.well-known/jwks.json"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	jwks, err = keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		panic("falha ao carregar JWKS: " + err.Error())
	}
}

// parseUserID valida o JWT e retorna o "sub" (userID).
// Retorna "" se o token for inválido ou não contiver sub.
func parseUserID(tokenStr string) string {
	token, err := jwt.Parse(tokenStr, jwks.Keyfunc)
	if err != nil || !token.Valid {
		return ""
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	sub, _ := claims["sub"].(string)
	return sub
}

// AuthMiddleware exige token válido — aborta com 401 se ausente ou inválido.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authorization header ausente"})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "formato inválido: use 'Bearer <token>'"})
			return
		}

		sub := parseUserID(parts[1])
		if sub == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token inválido ou expirado"})
			return
		}

		c.Set("userID", sub)
		c.Next()
	}
}

// OptionalAuth lê o token se presente e seta "userID" no contexto.
// Não bloqueia a requisição se não houver token ou se for inválido (guest).
func OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			tokenStr := strings.TrimPrefix(auth, "Bearer ")
			if userID := parseUserID(tokenStr); userID != "" {
				c.Set("userID", userID)
			}
		}
		c.Next()
	}
}