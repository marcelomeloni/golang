package client

type CreateMarketOrderRequest struct {
	ListingID  string `json:"listingId"  binding:"required"`
	BuyerName  string `json:"buyerName"`
	BuyerEmail string `json:"buyerEmail"`
	BuyerCPF   string `json:"buyerCPF"`
}

type CreateMarketOrderResponse struct {
	TransactionID     string  `json:"transactionId"`
	PaymentMethod     string  `json:"paymentMethod"`
	TotalAmount       float64 `json:"totalAmount"`
	PixCode           string  `json:"pixCode,omitempty"`
	PixQrCode         string  `json:"pixQrCode,omitempty"`
	IsGuest           bool    `json:"isGuest,omitempty"`
	ConfirmationEmail string  `json:"confirmationEmail,omitempty"`
}