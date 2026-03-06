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
}

type CreateOrderResponse struct {
	OrderID       string  `json:"orderId"`
	PaymentMethod string  `json:"paymentMethod"`
	TotalAmount   float64 `json:"totalAmount"`
	PixCode       string  `json:"pixCode,omitempty"`
	PixQrCode     string  `json:"pixQrCode,omitempty"`
}

type ValidateCouponRequest struct {
	EventID string `json:"eventId" binding:"required"`
	Code    string `json:"code"    binding:"required"`
}

type ValidateCouponResponse struct {
	Valid         bool    `json:"valid"`
	DiscountType  string  `json:"discountType,omitempty"`
	DiscountValue float64 `json:"discountValue,omitempty"`
	Message       string  `json:"message,omitempty"`
}