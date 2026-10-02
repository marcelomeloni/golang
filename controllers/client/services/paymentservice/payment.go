package paymentservice

type Result struct {
	PixCode    string
	PixQrCode  string
	ExternalID string
}

type Gateway interface {
	GeneratePix(orderID string, amountBRL float64, buyerName, buyerEmail, buyerCPF, buyerPhone string) (Result, error)
	CheckStatus(externalID string) (string, error)
	Withdraw(referenceID string, amountBRL float64, pixKey string, pixKeyType string) error
}

// Default é o gateway ativo. Inicialize em main.go com:
//
//	paymentservice.Default = paymentservice.NewAbacatePay(os.Getenv("ABACATEPAY_API_KEY"))
var Default Gateway