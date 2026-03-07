package paymentservice

// Result é retornado pelo gateway após criação da cobrança.
type Result struct {
	PixCode    string // código copia-e-cola (brCode)
	PixQrCode  string // imagem base64 do QRCode
	ExternalID string // ID da cobrança no AbacatePay (ex: "pix_char_123456")
}

// Gateway é a interface que qualquer provedor de pagamento deve implementar.
type Gateway interface {
	// GeneratePix cria uma cobrança Pix. Os campos buyer* são opcionais:
	// quando presentes e completos, são enviados como customer ao gateway.
	GeneratePix(orderID string, amountBRL float64, buyerName, buyerEmail, buyerCPF, buyerPhone string) (Result, error)
	CheckStatus(externalID string) (string, error)
}

// Default é o gateway ativo. Inicialize em main.go com:
//
//	paymentservice.Default = paymentservice.NewAbacatePay(os.Getenv("ABACATEPAY_API_KEY"))
var Default Gateway