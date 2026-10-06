package emailtemplates

import (
	"fmt"
	"os"
	"strings"
)

const refundTemplatePath = "templates/refund_status.html"

// RefundStatusData contém os dados do e-mail de status de reembolso.
type RefundStatusData struct {
	NomeUsuario string
	EventoNome  string
	// Status aceita: "received" | "approved" | "rejected"
	Status  string
	Amount  string
	CTAURL  string
	CTALabel string
}

type refundVariant struct {
	badge      string
	badgeColor string
	textHTML   string
	subject    string
	showAmount bool
}

func refundVariantFor(status string) refundVariant {
	switch status {
	case "approved":
		return refundVariant{
			badge:      "Reembolso aprovado",
			badgeColor: "#1BFF11",
			subject:    "Seu reembolso foi aprovado ✅",
			showAmount: true,
			textHTML: `<p style="margin:0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              Seu pedido de reembolso do evento <strong style="color:#F7F7F2;">{{EVENTO_NOME}}</strong>
              foi <strong style="color:#1BFF11;">aprovado</strong> pelo organizador.
            </p>
            <p style="margin:12px 0 0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              O valor será devolvido para a mesma forma de pagamento da compra
              (Pix) e pode levar até <strong style="color:#F7F7F2;">10 dias úteis</strong>
              para aparecer na sua conta, seguindo o prazo do banco.
            </p>`,
		}
	case "rejected":
		return refundVariant{
			badge:      "Solicitação recusada",
			badgeColor: "#F87171",
			subject:    "Sobre sua solicitação de reembolso",
			showAmount: false,
			textHTML: `<p style="margin:0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              Seu pedido de reembolso do evento <strong style="color:#F7F7F2;">{{EVENTO_NOME}}</strong>
              foi <strong style="color:#F87171;">recusado</strong> pelo organizador.
            </p>
            <p style="margin:12px 0 0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              Se tiver alguma dúvida, fale direto com a organização do evento.
            </p>`,
		}
	default: // received
		return refundVariant{
			badge:      "Solicitação recebida",
			badgeColor: "#F0B429",
			subject:    "Recebemos seu pedido de reembolso",
			showAmount: true,
			textHTML: `<p style="margin:0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              Recebemos seu pedido de reembolso do evento
              <strong style="color:#F7F7F2;">{{EVENTO_NOME}}</strong> e ele já está
              com o organizador para análise.
            </p>
            <p style="margin:12px 0 0;font-size:13px;color:#9A9A8F;line-height:1.7;">
              Você vai receber um novo e-mail assim que for aprovado ou recusado.
              O valor do reembolso segue a soma total paga no pedido.
            </p>`,
		}
	}
}

// BuildRefundStatus lê o template de status de reembolso e retorna o subject e o
// HTML prontos para envio, de acordo com o status informado.
func BuildRefundStatus(d RefundStatusData) (subject, html string, err error) {
	tmplBytes, err := os.ReadFile(refundTemplatePath)
	if err != nil {
		return "", "", fmt.Errorf("emailtemplates: ler template de reembolso: %w", err)
	}

	v := refundVariantFor(d.Status)

	ctaURL := d.CTAURL
	if ctaURL == "" {
		ctaURL = "https://reppy.app.br/meus-ingressos"
	}
	ctaLabel := d.CTALabel
	if ctaLabel == "" {
		ctaLabel = "Ver meus ingressos"
	}

	amountBlock := ""
	if v.showAmount {
		blockColor := "#1BFF11"
		label := "Valor solicitado"
		if d.Status == "approved" {
			label = "Valor a ser devolvido"
		}
		amountBlock = fmt.Sprintf(`<table width="100%%" cellpadding="0" cellspacing="0">
		          <tr>
		            <td style="padding:20px 32px;border-bottom:1px solid #1e1e1e;text-align:center;">
		              <p style="margin:0 0 4px;font-size:11px;font-weight:700;letter-spacing:0.1em;text-transform:uppercase;color:#5C5C52;">
		                %s
		              </p>
		              <p style="margin:0;font-size:32px;font-weight:900;color:%s;letter-spacing:-1px;">
		                %s
		              </p>
		            </td>
		          </tr>
		        </table>`, label, blockColor, d.Amount)
	}

	text := strings.NewReplacer("{{EVENTO_NOME}}", d.EventoNome).Replace(v.textHTML)

	r := strings.NewReplacer(
		"{{NOME}}",         d.NomeUsuario,
		"{{STATUS_BADGE}}",  v.badge,
		"{{BADGE_COLOR}}",   v.badgeColor,
		"{{EVENTO_NOME}}",   d.EventoNome,
		"{{STATUS_TEXT}}",   text,
		"{{AMOUNT_BLOCK}}",  amountBlock,
		"{{CTA_URL}}",       ctaURL,
		"{{CTA_LABEL}}",     ctaLabel,
	)

	return v.subject, r.Replace(string(tmplBytes)), nil
}