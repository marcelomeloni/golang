package feehelper

// AbacatePayFixedCost é o custo fixo por transação cobrado pelo gateway (em reais).
const AbacatePayFixedCost = 0.80

type tierFee struct {
	maxPrice   float64
	percentage float64
}

var tiers = []tierFee{
	{maxPrice: 10.00, percentage: 0.10},
	{maxPrice: 14.99, percentage: 0.08},
	{maxPrice: 0, percentage: 0.07},
}

var promoTiers = []tierFee{
	{maxPrice: 10.00, percentage: 0.10},
	{maxPrice: 15.00, percentage: 0.08},
	{maxPrice: 24.99, percentage: 0.06},
	{maxPrice: 49.99, percentage: 0.05},
	{maxPrice: 0, percentage: 0.04},
}

type FeeResult struct {
	TicketPrice   float64
	FeePercentage float64
	FeeAmount     float64
	GatewayFee    float64
	NetMargin     float64
	FinalPrice    float64
	IsPromo       bool
}

func CalcFee(ticketPriceBRL float64, isPromo bool) FeeResult {
	t := selectTier(ticketPriceBRL, isPromo)
	feeAmount := ticketPriceBRL * t.percentage
	netMargin := feeAmount - AbacatePayFixedCost
	return FeeResult{
		TicketPrice:   ticketPriceBRL,
		FeePercentage: t.percentage,
		FeeAmount:     Round2(feeAmount),
		GatewayFee:    AbacatePayFixedCost,
		NetMargin:     Round2(netMargin),
		FinalPrice:    Round2(ticketPriceBRL + feeAmount),
		IsPromo:       isPromo,
	}
}

func CalcOrderFee(ticketPrices []float64, isPromo bool) (totalFee float64, totalMargin float64) {
	for _, price := range ticketPrices {
		r := CalcFee(price, isPromo)
		totalFee += r.FeeAmount
		totalMargin += r.NetMargin
	}
	return Round2(totalFee), Round2(totalMargin)
}

func IsAboveFloor(ticketPriceBRL float64, isPromo bool) bool {
	return CalcFee(ticketPriceBRL, isPromo).NetMargin >= 0
}

func FloorPercentage(ticketPriceBRL float64) float64 {
	if ticketPriceBRL == 0 {
		return 0
	}
	return Round2(AbacatePayFixedCost / ticketPriceBRL)
}

// Round2 arredonda para 2 casas decimais. Exportada para uso em outros pacotes.
func Round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func selectTier(price float64, isPromo bool) tierFee {
	ts := tiers
	if isPromo {
		ts = promoTiers
	}
	for _, t := range ts {
		if t.maxPrice == 0 || price <= t.maxPrice {
			return t
		}
	}
	return ts[len(ts)-1]
}