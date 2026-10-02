package orderservice

import (
	"database/sql"
	"log"

	"bilheteria-api/services/feehelper"
)

// CalcPlatformFee calcula o total de taxas para os itens cujo fee_payer = "buyer".
// A taxa é determinada pelo feehelper com base no preço do ingresso e no promo_fee do evento.
func CalcPlatformFee(db *sql.DB, eventID string, batches map[string]BatchInfo, items []OrderItem) float64 {
	var promoFee bool
	err := db.QueryRow(`SELECT promo_fee FROM events WHERE id = $1`, eventID).Scan(&promoFee)
	if err != nil {
		log.Printf("CalcPlatformFee: erro ao buscar promo_fee para evento %s: %v", eventID, err)
		promoFee = false
	}

	var total float64
	for _, item := range items {
		b := batches[item.LotID]
		if b.FeePayer != "buyer" || b.Price == 0 {
			continue
		}
		result := feehelper.CalcFee(b.Price, promoFee)
		total += result.FeeAmount * float64(item.Qty)
	}
	return feehelper.Round2(total)
}