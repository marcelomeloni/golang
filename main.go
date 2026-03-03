package main

import (
	"log"
	"os"

	"bilheteria-api/config"
	"bilheteria-api/middleware"
	organizerRoutes "bilheteria-api/routes/organizer"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  .env não encontrado, usando variáveis de ambiente do sistema")
	}

	config.InitDB()
	middleware.InitJWKS()

	r := gin.Default()

	// ── CORS ─────────────────────────────────────────────────────────────────
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})
	// ─────────────────────────────────────────────────────────────────────────

	organizerRoutes.Register(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Servidor rodando em :%s\n", port)
	r.Run(":" + port)
}