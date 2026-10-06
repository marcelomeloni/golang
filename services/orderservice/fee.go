package orderservice

import "bilheteria-api/services/feehelper"

// CalcPlatformFee calcula o total de taxas para os itens cujo fee_payer = "buyer".
// A taxa é determinada pelo feehelper com base no preço do ingresso.
func CalcPlatformFee(batches map[string]BatchInfo, items []OrderItem) float64 {
	var total float64
	for _, item := range items {
		b := batches[item.LotID]
		if b.FeePayer != "buyer" || b.Price == 0 {
			continue
		}
		result := feehelper.CalcFee(b.Price)
		total += result.FeeAmount * float64(item.Qty)
	}
	return feehelper.Round2(total)
}