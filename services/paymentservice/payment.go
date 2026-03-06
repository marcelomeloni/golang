package paymentservice

import "fmt"

// Result é retornado pelo gateway após criação da cobrança.
type Result struct {
	PixCode    string
	PixQrCode  string
	ExternalID string // ID da cobrança no gateway
}

// Gateway é a interface que qualquer provedor de pagamento deve implementar.
type Gateway interface {
	GeneratePix(orderID string, amountBRL float64) (Result, error)
}

// ──────────────────────────────────────────────
// Stub — substituir por implementação real
// ──────────────────────────────────────────────
// Exemplo de uso quando integrar (ex: Asaas):
//
//   gateway := asaasgateway.New(os.Getenv("ASAAS_API_KEY"))
//   result, err := gateway.GeneratePix(orderID, grandTotal)

type stubGateway struct{}

func (s stubGateway) GeneratePix(orderID string, amountBRL float64) (Result, error) {
	// TODO: integrar com Mercado Pago / Asaas / PagSeguro
	return Result{}, fmt.Errorf("pagamento não implementado")
}

// Default é o gateway ativo. Troque por uma implementação real em main.go ou config.
var Default Gateway = stubGateway{}