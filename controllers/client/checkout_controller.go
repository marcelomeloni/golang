package client

import (
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/couponservice"
	"bilheteria-api/services/orderservice"
	"github.com/gin-gonic/gin"
)

// ValidateCoupon → POST /client/checkout/coupon
func ValidateCoupon(c *gin.Context) {
	var req ValidateCouponRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "eventId e code são obrigatórios"})
		return
	}

	coupon, userMsg, err := couponservice.Load(config.GetDB(), req.EventID, req.Code)
	if err != nil {
		log.Printf("ValidateCoupon: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao validar cupom"})
		return
	}
	if userMsg != "" {
		c.JSON(http.StatusOK, ValidateCouponResponse{Valid: false, Message: userMsg})
		return
	}

	c.JSON(http.StatusOK, ValidateCouponResponse{
		Valid:         true,
		DiscountType:  coupon.DiscountType,
		DiscountValue: coupon.DiscountValue,
	})
}

// CreateOrder → POST /client/checkout/orders
func CreateOrder(c *gin.Context) {
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}

	db := config.GetDB()

	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)

	userIDSQL, err := orderservice.ResolveUserID(db, userIDStr)
	if err != nil {
		log.Printf("CreateOrder ResolveUserID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao identificar usuário"})
		return
	}

	// ── 1. Lotes ─────────────────────────────────────────────────────────

	lotIDs := make([]string, len(req.Items))
	for i, item := range req.Items {
		lotIDs[i] = item.LotID
	}

	batches, err := orderservice.LoadBatches(db, req.EventID, lotIDs)
	if err != nil {
		log.Printf("CreateOrder LoadBatches: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar lotes"})
		return
	}

	items := make([]orderservice.OrderItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = orderservice.OrderItem{LotID: item.LotID, Qty: item.Qty}
	}

	if msg := orderservice.ValidateItems(batches, items); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}

	subtotal, allFree := orderservice.CalcSubtotal(batches, items)

	// ── 2. Cupom ─────────────────────────────────────────────────────────

	var appliedCoupon *couponservice.Coupon
	var discountAmount float64

	if req.CouponCode != "" {
		coupon, userMsg, err := couponservice.Load(db, req.EventID, req.CouponCode)
		if err != nil {
			log.Printf("CreateOrder coupon: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao validar cupom"})
			return
		}
		if userMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": userMsg})
			return
		}
		appliedCoupon = coupon
		discountAmount = couponservice.ApplyDiscount(subtotal, coupon)
	}

	// ── 3. Taxa de plataforma ────────────────────────────────────────────

	var platformFeeAmount float64
	if !allFree {
		platformFeeAmount = orderservice.CalcPlatformFee(db, req.EventID, batches, items)
	}

	grandTotal := subtotal - discountAmount + platformFeeAmount
	if grandTotal < 0 {
		grandTotal = 0
	}

	// ── 4. Persistir em transação ────────────────────────────────────────

	tx, err := db.Begin()
	if err != nil {
		log.Printf("CreateOrder tx: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	orderID, err := orderservice.Persist(tx, req.EventID, userIDSQL, appliedCoupon,
		items, grandTotal, discountAmount, platformFeeAmount, allFree)
	if err != nil {
		log.Printf("CreateOrder Persist: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar pedido"})
		return
	}

	if appliedCoupon != nil {
		if err := couponservice.IncrementUsage(tx, appliedCoupon.ID); err != nil {
			log.Printf("CreateOrder coupon increment: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("CreateOrder commit: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao finalizar pedido"})
		return
	}

	// ── 5. Gratuito → responde direto; pago → delega ao paymentService ───

	if allFree || grandTotal == 0 {
		// TODO: enviar e-mail de confirmação via emailsender
		c.JSON(http.StatusCreated, CreateOrderResponse{
			OrderID:       orderID,
			PaymentMethod: "manual",
			TotalAmount:   grandTotal,
		})
		return
	}

	// TODO: result, err := paymentservice.Default.GeneratePix(orderID, grandTotal)
	c.JSON(http.StatusAccepted, CreateOrderResponse{
		OrderID:       orderID,
		PaymentMethod: "pix",
		TotalAmount:   grandTotal,
	})
}