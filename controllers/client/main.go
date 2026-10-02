package main

import (
	"log"
	"os"

	"bilheteria-api/config"
	"bilheteria-api/middleware"
	"bilheteria-api/services/paymentservice"

	// Import dos Controllers (onde está a lógica e o InitEmailSender)
	clientControllers "bilheteria-api/controllers/client"

	// Import das Rotas
	clientRoutes "bilheteria-api/routes/client"
	organizerRoutes "bilheteria-api/routes/organizer"
	webhookRoutes "bilheteria-api/routes/webhooks"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 1. Carrega as variáveis de ambiente PRIMEIRO
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  .env não encontrado, usando variáveis de ambiente do sistema")
	}

	// 2. AGORA SIM inicializamos o serviço de e-mail (usando os controllers!)
	clientControllers.InitEmailSender()

	// 3. Inicializa os outros serviços
	config.InitDB()
	middleware.InitJWKS()

	// ── Gateways de pagamento ─────────────────────────────────────────────────
	apiKey := os.Getenv("ABACATEPAY_API_KEY")
	if apiKey == "" {
		log.Fatal("❌ ABACATEPAY_API_KEY não definida")
	}
	paymentservice.Default = paymentservice.NewAbacatePay(apiKey)

	r := gin.Default()

	// ── CORS ──────────────────────────────────────────────────────────────────
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
	clientRoutes.Register(r)
	webhookRoutes.Register(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Servidor rodando em :%s\n", port)
	r.Run(":" + port)
}