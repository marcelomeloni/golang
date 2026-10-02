package eventservice

// MapAvailability converte os valores do frontend para os CHECKs do banco.
//
//	"hidden"    → "private"
//	"guestlist" → "invite_only"
//	qualquer outro → "public"
func MapAvailability(v string) string {
	switch v {
	case "hidden":
		return "private"
	case "guestlist":
		return "invite_only"
	default:
		return "public"
	}
}

// MapSalesTrigger converte "batch" para "previous_batch_sold_out" quando há
// um lote anterior resolvido; caso contrário retorna "date".
func MapSalesTrigger(v string, prevBatchID *string) string {
	if v == "batch" && prevBatchID != nil {
		return "previous_batch_sold_out"
	}
	return "date"
}

// MapFeePayer converte "customer" (frontend) para "buyer" (banco).
func MapFeePayer(v string) string {
	if v == "organizer" {
		return "organizer"
	}
	return "buyer"
}