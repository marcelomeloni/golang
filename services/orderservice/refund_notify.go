package orderservice

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/emailtemplates"
)

func formatBRL(amount float64) string {
	return fmt.Sprintf("R$ %.2f", amount)
}

func emailFrom() string {
	from := os.Getenv("RESEND_FROM")
	if from == "" {
		from = "Reppy <noreply@reppy.app.br>"
	}
	return from
}

// SendEventCancelledEmails notifica todos os compradores (pedidos pagos) de um
// evento cancelado, orientando sobre o reembolso.
func SendEventCancelledEmails(db *sql.DB, sender emailsender.Sender, eventID string) error {
	rows, err := db.Query(`
		SELECT COALESCE(u.full_name, 'Visitante'), COALESCE(u.email, ''),
		       e.title, e.start_date, e.location
		FROM orders o
		JOIN users u ON u.id = o.user_id
		JOIN events e ON e.id = o.event_id
		WHERE o.event_id = $1 AND o.status = 'paid'
		  AND COALESCE(u.email, '') <> ''
		GROUP BY u.id, e.id, o.event_id
	`, eventID)
	if err != nil {
		return fmt.Errorf("SendEventCancelledEmails: query: %w", err)
	}
	defer rows.Close()

	loc := loadLocation()
	sent := 0
	var firstErr error

	for rows.Next() {
		var (
			name, email, title string
			startDate          sql.NullTime
			locationJSON       []byte
		)
		if err := rows.Scan(&name, &email, &title, &startDate, &locationJSON); err != nil {
			continue
		}

		subject, html, err := emailtemplates.BuildEventCancelled(emailtemplates.EventCancelledData{
			NomeUsuario:  name,
			EventoNome:   title,
			EventoData:   formatEmailDate(startDate, loc),
			EventoHora:   formatEmailTime(startDate, loc),
			EventoLocal:  formatVenueShort(parseLocationJSON(locationJSON)),
			ReembolsoNote: "O reembolso é feito pela Reppy: é só entrar em \"Meus ingressos\" e solicitar. " +
				"Pagamentos via Pix são reembolsados integralmente pelo valor total do pedido.",
		})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("SendEventCancelledEmails: template: %w", err)
			}
			continue
		}

		_, err = sender.Send(emailsender.Message{
			From:     emailFrom(),
			To:       []emailsender.Recipient{{Name: name, Email: email}},
			Subject:  subject,
			HTMLBody: html,
		})
		if err != nil {
			log.Printf("SendEventCancelledEmails: %s: %v", email, err)
			continue
		}
		sent++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("SendEventCancelledEmails: rows: %w", err)
	}
	if firstErr != nil {
		return firstErr
	}
	log.Printf("SendEventCancelledEmails: eventID=%s enviados=%d ✓", eventID, sent)
	return nil
}

// SendRefundStatusEmail avisa o comprador sobre o status do reembolso do pedido.
// status aceita "received" | "approved" | "rejected".
func SendRefundStatusEmail(db *sql.DB, sender emailsender.Sender, orderID, status string) error {
	var (
		name, email, title string
		amount             float64
	)
	err := db.QueryRow(`
		SELECT COALESCE(u.full_name, 'Visitante'), COALESCE(u.email, ''),
		       e.title, o.total_amount
		FROM orders o
		JOIN users u ON u.id = o.user_id
		JOIN events e ON e.id = o.event_id
		WHERE o.id = $1
	`, orderID).Scan(&name, &email, &title, &amount)
	if err != nil {
		return fmt.Errorf("SendRefundStatusEmail: orderID=%s: %w", orderID, err)
	}
	if email == "" {
		log.Printf("SendRefundStatusEmail: orderID=%s sem e-mail, pulando", orderID)
		return nil
	}

	subject, html, err := emailtemplates.BuildRefundStatus(emailtemplates.RefundStatusData{
		NomeUsuario: name,
		EventoNome:  title,
		Status:      status,
		Amount:      formatBRL(amount),
	})
	if err != nil {
		return fmt.Errorf("SendRefundStatusEmail: template: %w", err)
	}

	_, err = sender.Send(emailsender.Message{
		From:     emailFrom(),
		To:       []emailsender.Recipient{{Name: name, Email: email}},
		Subject:  subject,
		HTMLBody: html,
	})
	if err != nil {
		return fmt.Errorf("SendRefundStatusEmail: %s: %w", email, err)
	}

	log.Printf("SendRefundStatusEmail: orderID=%s status=%s enviado ✓", orderID, status)
	return nil
}