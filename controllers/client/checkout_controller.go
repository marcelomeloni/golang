package client

import (
	"fmt"
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/couponservice"
	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/orderservice"
	"bilheteria-api/services/paymentservice"
	"github.com/gin-gonic/gin"
)

var defaultEmailSender = emailsender.New("")

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

func CreateOrder(c *gin.Context) {
	var req CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}

	db := config.GetDB()

	userID, _ := c.Get("userID")
	userIDStr, _ := userID.(string)
	isGuest := userIDStr == ""

	guest := &orderservice.GuestInfo{
		Name:  req.BuyerName,
		Email: req.BuyerEmail,
		CPF:   req.BuyerCPF,
	}

	userIDSQL, conflict, err := orderservice.ResolveUserID(db, userIDStr, guest)
	if err != nil {
		log.Printf("CreateOrder ResolveUserID: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao identificar usuário"})
		return
	}
	if conflict != nil {
		var msg string
		switch conflict.Code {
		case "cpf_already_exists":
			msg = fmt.Sprintf("CPF já cadastrado no e-mail %s. Faça login para acessar seus ingressos.", conflict.MaskedEmail)
		case "email_already_exists":
			msg = fmt.Sprintf("E-mail %s já cadastrado. Faça login para acessar seus ingressos.", conflict.MaskedEmail)
		}
		c.JSON(http.StatusConflict, gin.H{"error": msg, "code": conflict.Code})
		return
	}

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

	var platformFeeAmount float64
	if !allFree {
		platformFeeAmount = orderservice.CalcPlatformFee(db, req.EventID, batches, items)
	}

	grandTotal := subtotal - discountAmount + platformFeeAmount
	if grandTotal < 0 {
		grandTotal = 0
	}

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

	if allFree || grandTotal == 0 {
		go func() {
			if err := orderservice.SendConfirmationEmail(db, defaultEmailSender, orderID); err != nil {
				log.Printf("CreateOrder SendConfirmationEmail orderID=%s: %v", orderID, err)
			}
		}()

		c.JSON(http.StatusCreated, CreateOrderResponse{
			OrderID:           orderID,
			PaymentMethod:     "manual",
			TotalAmount:       grandTotal,
			IsGuest:           isGuest,
			ConfirmationEmail: req.BuyerEmail,
		})
		return
	}

	pixResult, err := paymentservice.Default.GeneratePix(orderID, grandTotal, req.BuyerName, req.BuyerEmail, req.BuyerCPF, "")
	if err != nil {
		// Pedido já persistido — não revertemos para evitar perda de dados. O webhook de expiração limpa pedidos sem pagamento.
		log.Printf("CreateOrder GeneratePix orderID=%s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar cobrança Pix"})
		return
	}

	if err := orderservice.SavePixCharge(db, orderID, pixResult.ExternalID); err != nil {
		log.Printf("CreateOrder SavePixCharge orderID=%s: %v", orderID, err)
	}

	c.JSON(http.StatusAccepted, CreateOrderResponse{
		OrderID:           orderID,
		PaymentMethod:     "pix",
		TotalAmount:       grandTotal,
		IsGuest:           isGuest,
		ConfirmationEmail: req.BuyerEmail,
		PixCode:           pixResult.PixCode,
		PixQrCode:         pixResult.PixQrCode,
	})
}

func CheckPixStatus(c *gin.Context) {
	orderID := c.Param("orderID")

	externalID, err := orderservice.GetPixExternalID(config.GetDB(), orderID)
	if err != nil {
		log.Printf("CheckPixStatus GetPixExternalID orderID=%s: %v", orderID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "cobrança não encontrada"})
		return
	}

	status, err := paymentservice.Default.CheckStatus(externalID)
	if err != nil {
		log.Printf("CheckPixStatus CheckStatus orderID=%s: %v", orderID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao consultar status"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"orderID": orderID,
		"status":  status,
	})
}