package feehelper

// AbacatePayFixedCost é o custo fixo por transação cobrado pelo gateway (em reais).
const AbacatePayFixedCost = 0.80

// MinimumFee é o piso absoluto por ingresso: nunca cobramos menos que o custo do gateway.
const MinimumFee = 0.80

// FeeLowPriceThreshold é o limite de preço abaixo do qual aplicamos a taxa maior (10%).
const FeeLowPriceThreshold = 10.00

const (
	// StandardFeePercentage é a taxa para ingressos acima do limite (R$10).
	StandardFeePercentage = 0.08
	// LowPricePercentage é a taxa para ingressos até o limite (R$10), garantindo margem acima do piso.
	LowPricePercentage = 0.10
)

type FeeResult struct {
	TicketPrice   float64
	FeePercentage float64
	FeeAmount     float64
	GatewayFee    float64
	NetMargin     float64
	FinalPrice    float64
}

func CalcFee(ticketPriceBRL float64) FeeResult {
	if ticketPriceBRL <= 0 {
		return FeeResult{TicketPrice: ticketPriceBRL}
	}

	percentage := StandardFeePercentage
	if ticketPriceBRL <= FeeLowPriceThreshold {
		percentage = LowPricePercentage
	}

	feeAmount := ticketPriceBRL * percentage
	if feeAmount < MinimumFee {
		feeAmount = MinimumFee
	}
	feeAmount = Round2(feeAmount)

	return FeeResult{
		TicketPrice:   Round2(ticketPriceBRL),
		FeePercentage: Round2(feeAmount / ticketPriceBRL),
		FeeAmount:     feeAmount,
		GatewayFee:    AbacatePayFixedCost,
		NetMargin:     Round2(feeAmount - AbacatePayFixedCost),
		FinalPrice:    Round2(ticketPriceBRL + feeAmount),
	}
}

// Round2 arredonda para 2 casas decimais. Exportada para uso em outros pacotes.
func Round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}