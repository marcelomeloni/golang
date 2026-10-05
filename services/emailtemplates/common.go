package emailtemplates

// Helpers compartilhados pelos builders de e-mail.
//
// promoter_emission.go e ticket_transfer.go declaravam loteLabel e guestAlert
// com o mesmo nome no mesmo package, o que quebrava o build com "redeclared
// in this block". loteLabel é idêntico nos dois lados e vive aqui uma vez.
// guestAlert continua em duas variantes porque a mensagem muda conforme o
// contexto (ingresso emitido por promoter x ingresso transferido).

func loteLabel(lote string) string {
	if lote == "" {
		return "Ingresso"
	}
	return lote
}

func guestAlertPromoter(isGuest bool) string {
	if !isGuest {
		return ""
	}
	return `<div style="margin:0 0 20px;padding:14px 18px;background:#FFF8E1;border:1px solid #F0D68A;border-radius:12px;font-size:13px;color:#6B5A1F;line-height:1.6;">
              ⚠️ Este ingresso foi emitido por um promoter para uma conta de visitante. Se você não esperava este ingresso, não use o QR Code — fale com a gente.
            </div>`
}

// guestAlertTransfer destaca o caso em que o destinatário é uma conta de visitante.
// Guest não tem e-mail verificado nem senha, então é o perfil onde uma
// transferência errada passa mais tempo sem ninguém reclamar.
func guestAlertTransfer(isGuest bool) string {
	if !isGuest {
		return ""
	}
	return `<div style="margin:0 0 20px;padding:14px 18px;background:#FFF8E1;border:1px solid #F0D68A;border-radius:12px;font-size:13px;color:#6B5A1F;line-height:1.6;">
              ⚠️ Este ingresso foi enviado para uma conta de visitante, sem e-mail verificado. Se você não esperava este ingresso, não use o QR Code — fale com a gente.
            </div>`
}
