package client

// CheckoutItem representa um lote e a quantidade desejada.
type CheckoutItem struct {
	LotID string `json:"lotId" binding:"required"`
	Qty   int    `json:"qty"   binding:"required,min=1"`
}

type CreateOrderRequest struct {
	EventID    string         `json:"eventId"    binding:"required"`
	CouponCode string         `json:"couponCode"`
	Items      []CheckoutItem `json:"items"      binding:"required,min=1"`
	BuyerName  string         `json:"buyerName"`
	BuyerEmail string         `json:"buyerEmail"`
	BuyerCPF   string         `json:"buyerCPF"`
}

type CreateOrderResponse struct {
	OrderID           string  `json:"orderId"`
	PaymentMethod     string  `json:"paymentMethod"`
	TotalAmount       float64 `json:"totalAmount"`
	PixCode           string  `json:"pixCode,omitempty"`
	PixQrCode         string  `json:"pixQrCode,omitempty"`
	IsGuest           bool    `json:"isGuest,omitempty"`
	ConfirmationEmail string  `json:"confirmationEmail,omitempty"`
}

type ValidateCouponRequest struct {
	EventID  string  `json:"eventId"  binding:"required"`
	Code     string  `json:"code"     binding:"required"`
	Subtotal float64 `json:"subtotal" binding:"required"` 
}

type ValidateCouponResponse struct {
	Valid           bool    `json:"valid"`
	DiscountType    string  `json:"discountType,omitempty"`
	DiscountValue   float64 `json:"discountValue,omitempty"`
	DiscountAmount  float64 `json:"discountAmount,omitempty"`
	Message         string  `json:"message,omitempty"`
}