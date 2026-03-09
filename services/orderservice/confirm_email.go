package orderservice

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/emailtemplates"
	"bilheteria-api/services/ticketpdf"
)

// orderEmailData agrega os dados do pedido necessários para montar o e-mail.
type orderEmailData struct {
	BuyerName   string
	BuyerEmail  string
	EventoNome  string
	EventoData  string
	EventoHora  string
	EventoLocal string
	Tickets     []ticketEmailRow
}

type ticketEmailRow struct {
	ID       string
	QRCode   string
	LoteName string
	Status   string
	Evento   ticketpdf.EventData
}

// SendConfirmationEmail busca todos os ingressos do pedido, gera um PDF por ingresso
// e envia o e-mail de confirmação com os PDFs em anexo.
func SendConfirmationEmail(db *sql.DB, sender emailsender.Sender, orderID string) error {
	data, err := loadOrderEmailData(db, orderID)
	if err != nil {
		return fmt.Errorf("SendConfirmationEmail: carregar dados do pedido %s: %w", orderID, err)
	}

	if data.BuyerEmail == "" {
		log.Printf("SendConfirmationEmail: pedido %s sem e-mail, pulando envio", orderID)
		return nil
	}

	attachments, err := buildTicketAttachments(data.Tickets)
	if err != nil {
		return fmt.Errorf("SendConfirmationEmail: gerar PDFs do pedido %s: %w", orderID, err)
	}

	templateLines := toTemplateLines(data.Tickets)
	subject, html, err := emailtemplates.BuildTicketConfirmation(emailtemplates.TicketConfirmationData{
		NomeUsuario: data.BuyerName,
		EventoNome:  data.EventoNome,
		EventoData:  data.EventoData,
		EventoHora:  data.EventoHora,
		EventoLocal: data.EventoLocal,
		Tickets:     templateLines,
	})
	if err != nil {
		return fmt.Errorf("SendConfirmationEmail: montar template: %w", err)
	}

	fromAddress := os.Getenv("RESEND_FROM")
	if fromAddress == "" {
		fromAddress = "Reppy <noreply@reppy.app.br>"
	}

	_, err = sender.Send(emailsender.Message{
		From:        fromAddress,
		To:          []emailsender.Recipient{{Name: data.BuyerName, Email: data.BuyerEmail}},
		Subject:     subject,
		HTMLBody:    html,
		Attachments: attachments,
	})
	return err
}

func buildTicketAttachments(tickets []ticketEmailRow) ([]emailsender.Attachment, error) {
	attachments := make([]emailsender.Attachment, 0, len(tickets))

	for _, t := range tickets {
		pdfBytes, err := ticketpdf.Generate(ticketpdf.TicketData{
			ID:       t.ID,
			QRCode:   t.QRCode,
			LoteName: t.LoteName,
			Status:   t.Status,
			Evento:   t.Evento,
		})
		if err != nil {
			log.Printf("buildTicketAttachments: gerar PDF do ingresso %s: %v", t.ID, err)
			continue
		}

		attachments = append(attachments, emailsender.Attachment{
			Filename:    fmt.Sprintf("ingresso-%s.pdf", t.QRCode),
			Content:     pdfBytes,
			ContentType: "application/pdf",
		})
	}

	if len(attachments) == 0 {
		return nil, fmt.Errorf("nenhum PDF gerado com sucesso para o pedido")
	}

	return attachments, nil
}

const orderEmailQuery = `
	SELECT
		COALESCE(u.full_name, 'Visitante')  AS buyer_name,
		COALESCE(u.email, '')               AS buyer_email,
		e.title                             AS evento_nome,
		e.start_date,
		e.location,
		t.id                                AS ticket_id,
		t.qr_code,
		t.status,
		COALESCE(tb.name, '')               AS lote_name,
		COALESCE(e.image_url, '')           AS image_url
	FROM orders o
	JOIN users            u  ON u.id  = o.user_id
	JOIN events           e  ON e.id  = o.event_id
	JOIN tickets          t  ON t.order_id = o.id
	LEFT JOIN ticket_batches tb ON tb.id = t.batch_id
	WHERE o.id     = $1
	  AND o.status = 'paid'
	  AND t.status != 'cancelled'
	ORDER BY t.created_at ASC`

func loadOrderEmailData(db *sql.DB, orderID string) (*orderEmailData, error) {
	rows, err := db.Query(orderEmailQuery, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result *orderEmailData
	loc := loadLocation()

	for rows.Next() {
		var (
			buyerName    string
			buyerEmail   string
			eventoNome   string
			startDate    sql.NullTime
			locationJSON []byte
			ticketID     string
			qrCode       string
			status       string
			loteName     string
			imageURL     string
		)

		if err := rows.Scan(
			&buyerName, &buyerEmail,
			&eventoNome, &startDate, &locationJSON,
			&ticketID, &qrCode, &status, &loteName, &imageURL,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

	
		var addr struct {
			VenueName    string `json:"venue_name"`
			Street       string `json:"street"`
			Number       string `json:"number"`
			Neighborhood string `json:"neighborhood"`
			City         string `json:"city"`
			State        string `json:"state"`
			CEP          string `json:"cep"`
		}
		
		if len(locationJSON) > 0 {
			_ = json.Unmarshal(locationJSON, &addr)
		}

		// (Opcional, mas seguro): Formata a string curta do local para o e-mail em si
		localCurto := "Local a definir"
		if addr.VenueName != "" && addr.City != "" {
			localCurto = fmt.Sprintf("%s, %s", addr.VenueName, addr.City)
		} else if addr.VenueName != "" {
			localCurto = addr.VenueName
		} else if addr.City != "" {
			localCurto = addr.City
		}

		if result == nil {
			result = &orderEmailData{
				BuyerName:   buyerName,
				BuyerEmail:  buyerEmail,
				EventoNome:  eventoNome,
				EventoData:  formatEmailDate(startDate, loc),
				EventoHora:  formatEmailTime(startDate, loc),
				EventoLocal: localCurto,
			}
		}

		result.Tickets = append(result.Tickets, ticketEmailRow{
			ID:       ticketID,
			QRCode:   qrCode,
			LoteName: loteName,
			Status:   status,
			Evento: ticketpdf.EventData{
				Nome:         eventoNome,
				Data:         result.EventoData,
				Hora:         result.EventoHora,
				VenueName:    addr.VenueName,
				Street:       addr.Street,
				Number:       addr.Number,
				Neighborhood: addr.Neighborhood,
				City:         addr.City,
				State:        addr.State,
				CEP:          addr.CEP,
				ImageURL:     imageURL,
			},
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("pedido %s não encontrado ou não está pago", orderID)
	}

	return result, nil
}

func toTemplateLines(tickets []ticketEmailRow) []emailtemplates.TicketLine {
	lines := make([]emailtemplates.TicketLine, len(tickets))
	for i, t := range tickets {
		lines[i] = emailtemplates.TicketLine{
			QRCode:   t.QRCode,
			LoteName: t.LoteName,
		}
	}
	return lines
}