package orderservice

import (
	"database/sql"
	"fmt"
	"log"

	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/emailtemplates"
	"bilheteria-api/services/ticketpdf"
)

const fromAddress = "Reppy <noreply@reppy.com.br>"

// orderEmailData agrega os dados do pedido necessários para montar o e-mail.
type orderEmailData struct {
	BuyerName  string
	BuyerEmail string
	EventoNome string
	EventoData string
	EventoHora string
	EventoLocal string
	Tickets    []ticketEmailRow
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
//
// Deve ser chamado logo após o pedido ser marcado como "paid" —
// tanto para pedidos gratuitos (direto no checkout) quanto para pedidos pagos
// (a partir do webhook de confirmação do Pix).
func SendConfirmationEmail(db *sql.DB, sender emailsender.Sender, orderID string) error {
	data, err := loadOrderEmailData(db, orderID)
	if err != nil {
		return fmt.Errorf("SendConfirmationEmail: carregar dados do pedido %s: %w", orderID, err)
	}

	if data.BuyerEmail == "" {
		// Pedido de guest sem e-mail — não há destinatário, nada a fazer.
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

	_, err = sender.Send(emailsender.Message{
		From:        fromAddress,
		To:          []emailsender.Recipient{{Name: data.BuyerName, Email: data.BuyerEmail}},
		Subject:     subject,
		HTMLBody:    html,
		Attachments: attachments,
	})
	return err
}

// ──────────────────────────────────────────────
// Geração dos anexos PDF
// ──────────────────────────────────────────────

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
			// Falha em um ingresso não impede o envio dos demais — logamos e continuamos.
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

// ──────────────────────────────────────────────
// Query e scan
// ──────────────────────────────────────────────

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

	for rows.Next() {
		var (
			buyerName   string
			buyerEmail  string
			eventoNome  string
			startDate   sql.NullTime
			locationJSON []byte
			ticketID    string
			qrCode      string
			status      string
			loteName    string
			imageURL    string
		)

		if err := rows.Scan(
			&buyerName, &buyerEmail,
			&eventoNome, &startDate, &locationJSON,
			&ticketID, &qrCode, &status, &loteName, &imageURL,
		); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		// Inicializa o resultado com os dados do pedido na primeira linha.
		if result == nil {
			loc := loadLocation()
			addr := parseLocationJSON(locationJSON)

			result = &orderEmailData{
				BuyerName:   buyerName,
				BuyerEmail:  buyerEmail,
				EventoNome:  eventoNome,
				EventoData:  formatEmailDate(startDate, loc),
				EventoHora:  formatEmailTime(startDate, loc),
				EventoLocal: formatVenueShort(addr),
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
				VenueName:    parseLocationJSON(locationJSON).VenueName,
				Street:       parseLocationJSON(locationJSON).Street,
				Number:       parseLocationJSON(locationJSON).Number,
				Neighborhood: parseLocationJSON(locationJSON).Neighborhood,
				City:         parseLocationJSON(locationJSON).City,
				State:        parseLocationJSON(locationJSON).State,
				CEP:          parseLocationJSON(locationJSON).CEP,
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

// ──────────────────────────────────────────────
// Conversão para o formato do template
// ──────────────────────────────────────────────

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