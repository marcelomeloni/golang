package orderservice

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/emailtemplates"
	"bilheteria-api/services/ticketpdf"
)

// TransferNotificationInput são os dados já coletados pela camada HTTP na
// hora da transferência. Tudo que a camada HTTP já tinha em mão vem aqui —
// a função só volta ao banco para o que falta (data, endereço e imagem do
// evento), evitando repetir consultas dentro do request.
type TransferNotificationInput struct {
	RecipientID    string
	RecipientName  string
	RecipientEmail string
	RecipientGuest bool
	SenderCPF      string
	EventID        string
	EventName      string
	TicketID       string
	LoteName       string
	NewQRCode      string
}

// transferEventQuery busca o que a camada HTTP não tinha: quando e onde o
// evento acontece, e a imagem para o PDF do ingresso.
const transferEventQuery = `
	SELECT e.start_date, e.location, COALESCE(e.image_url, '')
	FROM events e
	WHERE e.id = $1`

// SendTransferNotification avisa o destinatário de que recebeu um ingresso.
//
// É chamado DEPOIS do commit da transferência, nunca dentro dela: enviar
// e-mail segura conexão de rede por segundos e o erro mais provável (API do
// Resend fora) não pode derrubar uma transferência já persistida.
//
// Falha aqui é sempre não-fatal para o usuário — o ingresso está transferido
// no banco e o destinatário ainda o encontra em /meus-ingressos. O que se
// perde é o aviso, e é por isso que o erro só vai para o log.
func SendTransferNotification(db *sql.DB, sender emailsender.Sender, in TransferNotificationInput) error {
	if in.RecipientEmail == "" {
		log.Printf("SendTransferNotification: destinatário %s sem e-mail, pulando envio", in.RecipientID)
		return nil
	}
	if sender == nil {
		log.Printf("SendTransferNotification: sender não inicializado, pulando envio para %s", in.RecipientID)
		return nil
	}

	var (
		startDate    sql.NullTime
		locationJSON []byte
		imageURL     string
	)
	if err := db.QueryRow(transferEventQuery, in.EventID).
		Scan(&startDate, &locationJSON, &imageURL); err != nil {
		return fmt.Errorf("SendTransferNotification: carregar evento %s: %w", in.EventID, err)
	}

	addr := parseLocationJSON(locationJSON)
	loc := loadLocation()
	eventoData := formatEmailDate(startDate, loc)
	eventoHora := formatEmailTime(startDate, loc)

	subject, html, err := emailtemplates.BuildTicketTransfer(emailtemplates.TransferData{
		NomeUsuario:  in.RecipientName,
		EventoNome:   in.EventName,
		EventoData:   eventoData,
		EventoHora:   eventoHora,
		EventoLocal:  formatVenueShort(addr),
		LoteNome:     in.LoteName,
		QRCode:       in.NewQRCode,
		SenderCPF:    in.SenderCPF,
		GuestAccount: in.RecipientGuest,
	})
	if err != nil {
		return fmt.Errorf("SendTransferNotification: montar template: %w", err)
	}

	// O PDF é o mesmo que o usuário baixaria no app, então o anexo e o
	// QR da tela não podem divergir. Falha aqui não cancela o e-mail:
	// perder o anexo é melhor do que perder o aviso inteiro.
	var attachments []emailsender.Attachment
	if pdfBytes, err := ticketpdf.Generate(ticketpdf.TicketData{
		ID:       in.TicketID,
		QRCode:   in.NewQRCode,
		LoteName: in.LoteName,
		Status:   "valid",
		Evento: ticketpdf.EventData{
			Nome:         in.EventName,
			Data:         eventoData,
			Hora:         eventoHora,
			VenueName:    addr.VenueName,
			Street:       addr.Street,
			Number:       addr.Number,
			Neighborhood: addr.Neighborhood,
			City:         addr.City,
			State:        addr.State,
			CEP:          addr.CEP,
			ImageURL:     imageURL,
		},
	}); err != nil {
		log.Printf("SendTransferNotification: gerar PDF do ingresso %s: %v", in.TicketID, err)
	} else {
		attachments = []emailsender.Attachment{{
			Filename:    fmt.Sprintf("ingresso-%s.pdf", in.NewQRCode),
			Content:     pdfBytes,
			ContentType: "application/pdf",
		}}
	}

	fromAddress := os.Getenv("RESEND_FROM")
	if fromAddress == "" {
		fromAddress = "Reppy <noreply@reppy.app.br>"
	}

	name := in.RecipientName
	if name == "" {
		name = "Reppy"
	}

	_, err = sender.Send(emailsender.Message{
		From:        fromAddress,
		To:          []emailsender.Recipient{{Name: name, Email: in.RecipientEmail}},
		Subject:     subject,
		HTMLBody:    html,
		Attachments: attachments,
	})
	if err != nil {
		return fmt.Errorf("SendTransferNotification: enviar para %s: %w", in.RecipientEmail, err)
	}
	return nil
}