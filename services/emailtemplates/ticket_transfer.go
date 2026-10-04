package emailtemplates

import (
	"fmt"
	"os"
	"strings"
)

const transferTemplatePath = "templates/ticket_transfer.html"

// TransferData contém os dados necessários para o e-mail de transferência
// de ingresso, disparado para o destinatário logo após a troca de dono.
// Sempre descreve exatamente um ingresso — a API transfere um por vez.
type TransferData struct {
	NomeUsuario  string
	EventoNome   string
	EventoData   string
	EventoHora   string
	EventoLocal  string
	LoteNome     string
	QRCode       string
	SenderCPF    string
	GuestAccount bool
}

// BuildTicketTransfer lê o template HTML do disco e retorna o subject e o HTML
// com todas as variáveis substituídas.
func BuildTicketTransfer(d TransferData) (subject, html string, err error) {
	tmplBytes, err := os.ReadFile(transferTemplatePath)
	if err != nil {
		return "", "", fmt.Errorf("emailtemplates: ler template: %w", err)
	}

	subject = fmt.Sprintf("Você recebeu um ingresso para %s 🎁", d.EventoNome)

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
		"{{CPF_REMITENTE}}",   senderLabel(d.SenderCPF),
		"{{GUEST_ALERT}}",     guestAlert(d.GuestAccount),
		"{{ATTACHMENT_NOTE}}", transferAttachmentNote(),
	)

	return subject, r.Replace(string(tmplBytes)), nil
}

func loteLabel(lote string) string {
	if lote == "" {
		return "Ingresso"
	}
	return lote
}

// senderLabel identifica o remetente.
//
// Com CPF: mascarado. Mandar o CPF inteiro por e-mail para um terceiro
// expõe dado pessoal sem necessidade — o destinatário só precisa saber de
// quem veio, e o remetente não precisa ter o dado circulando por e-mail.
// Sem CPF (conta sem cadastro completo): rótulo genérico.
func senderLabel(cpf string) string {
	if len(cpf) != 11 {
		return "outra conta Reppy"
	}
	return "CPF " + cpf[0:3] + ".***.***-" + cpf[9:11]
}

// guestAlert destaca o caso em que o destinatário é uma conta de visitante.
// Guest não tem e-mail verificado nem senha, então é o perfil onde uma
// transferência errada passa mais tempo sem ninguém reclamar.
func guestAlert(isGuest bool) string {
	if !isGuest {
		return ""
	}
	return `<div style="margin:0 0 20px;padding:14px 18px;background:#FFF8E1;border:1px solid #F0D68A;border-radius:12px;font-size:13px;color:#6B5A1F;line-height:1.6;">
              ⚠️ Este ingresso foi enviado para uma conta de visitante, sem e-mail verificado. Se você não esperava este ingresso, não use o QR Code — fale com a gente.
            </div>`
}

// transferAttachmentNote é a nota de anexo. Nome diferente do attachmentNote
// de ticket_confirmation.go porque aquele é plural e parametrizado pelo
// número de ingressos do pedido — aqui é sempre exatamente um.
func transferAttachmentNote() string {
	return "O ingresso em PDF está em anexo neste e-mail."
}