package feehelper

// FeeLowPriceThreshold é o limite de preço abaixo do qual aplicamos a taxa maior (10%).
const FeeLowPriceThreshold = 10.00

const (
	// StandardFeePercentage é a taxa para ingressos acima do limite (R$10).
	StandardFeePercentage = 0.08
	// LowPricePercentage é a taxa para ingressos até o limite (R$10).
	LowPricePercentage = 0.10
)

type FeeResult struct {
	TicketPrice   float64
	FeePercentage float64
	FeeAmount     float64
	FinalPrice    float64
}

// CalcFee calcula a taxa de um ingresso.
//
// orgPercentage é o percentual da organização como fração (ex.: 0.08 = 8%),
// vindo de organizations.platform_fee_percentage. Se <= 0, cai no padrão
// global (StandardFeePercentage). Ingressos até FeeLowPriceThreshold mantêm
// LowPricePercentage (10%), independente da organização.
func CalcFee(ticketPriceBRL, orgPercentage float64) FeeResult {
	if ticketPriceBRL <= 0 {
		return FeeResult{TicketPrice: ticketPriceBRL}
	}

	percentage := orgPercentage
	if percentage <= 0 {
		percentage = StandardFeePercentage
	}
	if ticketPriceBRL <= FeeLowPriceThreshold {
		percentage = LowPricePercentage
	}

	feeAmount := Round2(ticketPriceBRL * percentage)

	return FeeResult{
		TicketPrice:   Round2(ticketPriceBRL),
		FeePercentage: Round2(feeAmount / ticketPriceBRL),
		FeeAmount:     feeAmount,
		FinalPrice:    Round2(ticketPriceBRL + feeAmount),
	}
}

// Round2 arredonda para 2 casas decimais. Exportada para uso em outros pacotes.
func Round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}