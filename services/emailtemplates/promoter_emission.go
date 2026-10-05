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
		"{{GUEST_ALERT}}",     guestAlertPromoter(true),
		"{{ATTACHMENT_NOTE}}", promoterAttachmentNote(),
	)

	return subject, r.Replace(string(tmplBytes)), nil
}

func promoterLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "um promoter do evento"
	}
	return name
}

func promoterAttachmentNote() string {
	return "O ingresso em PDF está em anexo neste e-mail."
}
