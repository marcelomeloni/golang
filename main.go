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
	adminRoutes "bilheteria-api/routes/admin"
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
	// Migrado para C6 Bank
	c6ClientID := os.Getenv("C6_CLIENT_ID")
	c6ClientSecret := os.Getenv("C6_CLIENT_SECRET")
	c6PixKey := os.Getenv("C6_PIX_KEY")
	c6Env := os.Getenv("C6_ENV")
	if c6ClientID == "" || c6ClientSecret == "" || c6PixKey == "" {
		log.Fatal("❌ C6_CLIENT_ID, C6_CLIENT_SECRET ou C6_PIX_KEY não definidas")
	}
	gw, err := paymentservice.NewC6Bank(c6ClientID, c6ClientSecret, c6PixKey, c6Env)
	if err != nil {
		log.Fatal(err)
	}
	paymentservice.Default = gw

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
	webhookRoutes.RegisterC6(r)
	adminRoutes.Register(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Servidor rodando em :%s\n", port)
	r.Run(":" + port)
}