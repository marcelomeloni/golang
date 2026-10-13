package orderservice

import "bilheteria-api/services/feehelper"

// CalcPlatformFees calcula as taxas da plataforma para os itens do pedido.
//
// O percentual vem da organização (orgPercentage, fração, ex.: 0.08 = 8%) e o
// feehelper aplica o piso e a regra de preço baixo.
//
// Retorna dois valores, porque a taxa pode ser paga pelo comprador ou pela
// organização (ticket_batches.fee_payer):
//   - buyerFee: soma das taxas dos lotes com fee_payer = "buyer". É somada ao
//     total pago pelo comprador (grandTotal = subtotal - desconto + buyerFee).
//   - totalFee: soma de TODAS as taxas (buyer + organizer). É gravada em
//     orders.platform_fee_amount — ou seja, é o quanto a plataforma arrecada.
//
// O líquido da organização é grandTotal - totalFee (ver Persist). Assim, quando
// fee_payer = "organizer", a taxa sai do repasse da org; quando é "buyer", quem
// paga é o comprador e o net da org não é afetado.
func CalcPlatformFees(batches map[string]BatchInfo, items []OrderItem, orgPercentage float64) (buyerFee, totalFee float64) {
	for _, item := range items {
		b := batches[item.LotID]
		if b.Price <= 0 {
			continue
		}

		result := feehelper.CalcFee(b.Price, orgPercentage)
		lineTotal := result.FeeAmount * float64(item.Qty)

		totalFee += lineTotal
		if b.FeePayer == "buyer" {
			buyerFee += lineTotal
		}
	}
	return feehelper.Round2(buyerFee), feehelper.Round2(totalFee)
}
