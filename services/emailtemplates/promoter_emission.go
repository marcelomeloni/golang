package emailtemplates

import (
	"fmt"
	"os"
	"strings"
)

const promoterEmissionTemplatePath = "templates/promoter_emission.html"

type PromoterEmissionData struct {
	NomeUsuario  string
	EventoNome   string
	EventoData   string
	EventoHora   string
	EventoLocal  string
	LoteNome     string
	QRCode       string
	PromoterName string
}

func BuildPromoterEmission(d PromoterEmissionData) (subject, html string, err error) {
	tmplBytes, err := os.ReadFile(promoterEmissionTemplatePath)
	if err != nil {
		return "", "", fmt.Errorf("emailtemplates: ler template promoter: %w", err)
	}

	subject = fmt.Sprintf("Você recebeu um ingresso para %s 🎫", d.EventoNome)

	name := strings.TrimSpace(d.NomeUsuario)
	if name == "" {
		name = "você"
	}

	r := strings.NewReplacer(
		"{{NOME}}",            name,
		"{{EVENTO_NOME}}",     d.EventoNome,
		"{{EVENTO_DATA}}",     d.EventoData,
		"{{EVENTO_HORA}}",     d.EventoHora,
		"{{EVENTO_LOCAL}}",    d.EventoLocal,
		"{{LOTE}}",            loteLabel(d.LoteNome),
		"{{QR_CODE}}",         d.QRCode,
		"{{PROMOTER_NAME}}",   promoterLabel(d.PromoterName),
		"{{GUEST_ALERT}}",     guestAlert(true),
		"{{ATTACHMENT_NOTE}}", promoterAttachmentNote(),
	)

	return subject, r.Replace(string(tmplBytes)), nil
}

func loteLabel(lote string) string {
	if lote == "" {
		return "Ingresso"
	}
	return lote
}

func promoterLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "um promoter do evento"
	}
	return name
}

func guestAlert(isGuest bool) string {
	if !isGuest {
		return ""
	}
	return `<div style="margin:0 0 20px;padding:14px 18px;background:#FFF8E1;border:1px solid #F0D68A;border-radius:12px;font-size:13px;color:#6B5A1F;line-height:1.6;">
              ⚠️ Este ingresso foi emitido por um promoter para uma conta de visitante. Se você não esperava este ingresso, não use o QR Code — fale com a gente.
            </div>`
}

func promoterAttachmentNote() string {
	return "O ingresso em PDF está em anexo neste e-mail."
}