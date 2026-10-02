// Package emailtemplates carrega e prepara os templates de e-mail da Reppy.
// O HTML de cada template vive em templates/*.html — separado do código Go.
package emailtemplates

import (
	"fmt"
	"os"
	"strings"
)

const confirmationTemplatePath = "templates/ticket_confirmation.html"

// TicketLine representa um ingresso individual dentro de um pedido.
type TicketLine struct {
	QRCode   string
	LoteName string
}

// TicketConfirmationData contém os dados necessários para o e-mail de confirmação.
type TicketConfirmationData struct {
	NomeUsuario string
	EventoNome  string
	EventoData  string
	EventoHora  string
	EventoLocal string
	Tickets     []TicketLine
}

// BuildTicketConfirmation lê o template HTML do disco e retorna o subject e o HTML
// com todas as variáveis substituídas, pronto para ser enviado pelo emailsender.
func BuildTicketConfirmation(d TicketConfirmationData) (subject, html string, err error) {
	tmplBytes, err := os.ReadFile(confirmationTemplatePath)
	if err != nil {
		return "", "", fmt.Errorf("emailtemplates: ler template: %w", err)
	}

	ticketCount := len(d.Tickets)

	subject = buildSubject(d.EventoNome, ticketCount)

	r := strings.NewReplacer(
		"{{NOME}}",             d.NomeUsuario,
		"{{EVENTO_NOME}}",      d.EventoNome,
		"{{EVENTO_DATA}}",      d.EventoData,
		"{{EVENTO_HORA}}",      d.EventoHora,
		"{{EVENTO_LOCAL}}",     d.EventoLocal,
		"{{INGRESSO_LABEL}}",   ingressoLabel(ticketCount),
		"{{TICKETS_COUNT_LABEL}}", ticketsCountLabel(ticketCount),
		"{{TICKETS_LIST}}",     buildTicketsList(d.Tickets),
		"{{ATTACHMENT_NOTE}}",  attachmentNote(ticketCount),
	)

	return subject, r.Replace(string(tmplBytes)), nil
}

// ──────────────────────────────────────────────
// Helpers de texto
// ──────────────────────────────────────────────

func buildSubject(eventoNome string, count int) string {
	if count == 1 {
		return fmt.Sprintf("Seu ingresso para %s chegou 🎉", eventoNome)
	}
	return fmt.Sprintf("Seus %d ingressos para %s chegaram 🎉", count, eventoNome)
}

func ingressoLabel(count int) string {
	if count == 1 {
		return "ingresso confirmado"
	}
	return fmt.Sprintf("%d ingressos confirmados", count)
}

func ticketsCountLabel(count int) string {
	if count == 1 {
		return "seu ingresso"
	}
	return fmt.Sprintf("seus %d ingressos", count)
}

func attachmentNote(count int) string {
	if count == 1 {
		return "Seu ingresso em PDF está em anexo neste e-mail."
	}
	return fmt.Sprintf("Seus %d ingressos em PDF estão em anexo neste e-mail.", count)
}

// buildTicketsList gera as linhas HTML de cada ingresso para inserir no template.
func buildTicketsList(tickets []TicketLine) string {
	var sb strings.Builder

	for _, t := range tickets {
		sb.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="margin-bottom:10px;">`)
		sb.WriteString(`<tr>`)
		sb.WriteString(`<td style="background:#1a1a1a;border:1px solid #2a2a2a;border-radius:12px;padding:12px 16px;">`)

		// Lote
		sb.WriteString(fmt.Sprintf(
			`<span style="font-size:12px;font-weight:700;color:#9A9A8F;letter-spacing:0.08em;text-transform:uppercase;">%s</span>`,
			t.LoteName,
		))

		// QR Code
		sb.WriteString(fmt.Sprintf(
			`<span style="float:right;font-size:11px;font-weight:600;color:#5C5C52;letter-spacing:0.1em;">%s</span>`,
			t.QRCode,
		))

		sb.WriteString(`</td></tr></table>`)
	}

	return sb.String()
}