package promoterservice

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/emailtemplates"
	"bilheteria-api/services/orderservice"
	"github.com/google/uuid"
)

type EmitRequestItem struct {
	CPF          string `json:"cpf" binding:"required"`
	LotID        string `json:"lot_id" binding:"required"`
	Quantity     int    `json:"quantity" binding:"required,gt=0"`
	Email        string `json:"email"`
	Phone        string `json:"phone"`
	FullName     string `json:"full_name"`
}

type EmitRequest struct {
	Items []EmitRequestItem `json:"items" binding:"required,min=1,dive"`
}

type EmittedTicket struct {
	TicketID      string `json:"ticket_id"`
	QRCode        string `json:"qr_code"`
	RecipientUserID string `json:"recipient_user_id"`
	RecipientName string `json:"recipient_name"`
	RecipientEmail string `json:"recipient_email"`
	BatchName     string `json:"batch_name"`
	BatchType     string `json:"batch_type"`
}

type EmitResult struct {
	OrderID        string          `json:"order_id"`
	EmittedTickets []EmittedTicket `json:"emitted_tickets"`
	DistributionID string          `json:"distribution_id"`
}

func cleanCPF(cpf string) string {
	replacer := strings.NewReplacer(".", "", "-", "", " ", "")
	return replacer.Replace(cpf)
}

// CleanCPF normaliza um CPF para apenas dígitos. Exportado para os handlers
// que precisam validar/consultar CPF antes da emissão.
func CleanCPF(cpf string) string {
	return cleanCPF(cpf)
}

func cleanPhone(phone string) string {
	replacer := strings.NewReplacer("(", "", ")", "", "-", "", " ", "")
	return replacer.Replace(phone)
}

func EmitPromoterTickets(
	ctx context.Context,
	db *sql.DB,
	organizationID string,
	eventID string,
	promoterUserID string,
	items []EmitRequestItem,
	emailSender emailsender.Sender,
) (*EmitResult, error) {

	if len(items) == 0 {
		return nil, fmt.Errorf("nenhum item para emitir")
	}

	// 1. Validate event belongs to organization and get batch info
	batchIDs := make([]string, 0, len(items))
	for _, item := range items {
		batchIDs = append(batchIDs, item.LotID)
	}

	placeholders := make([]string, len(batchIDs))
	args := make([]interface{}, len(batchIDs)+2)
	args[0] = eventID
	args[1] = organizationID
	for i, id := range batchIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		args[i+2] = id
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT 
			tb.id,
			tb.name,
			tb.type,
			tb.price,
			tb.quantity_total,
			tb.quantity_sold,
			tb.max_purchase,
			e.title,
			e.start_date,
			e.end_date,
			e.location->>'venue_name' as venue_name,
			e.organization_id
		FROM ticket_batches tb
		JOIN events e ON e.id = tb.event_id
		WHERE tb.event_id = $1
		  AND e.organization_id = $2
		  AND tb.id IN (%s)
		  AND tb.status = 'active'
		  AND (tb.start_date IS NULL OR tb.start_date <= NOW())
		  AND (tb.end_date IS NULL OR tb.end_date > NOW())
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, fmt.Errorf("buscar lotes: %w", err)
	}
	defer rows.Close()

	batchMap := make(map[string]struct {
		Name            string
		Type            string
		Price           float64
		QuantityTotal   int
		QuantitySold    int
		MaxPurchase     int
		EventTitle      string
		EventStartDate  sql.NullTime
		EventEndDate    sql.NullTime
		EventVenue      sql.NullString
		OrganizationID  string
	})

	for rows.Next() {
		var b struct {
			Name            string
			Type            string
			Price           float64
			QuantityTotal   int
			QuantitySold    int
			MaxPurchase     int
			EventTitle      string
			EventStartDate  sql.NullTime
			EventEndDate    sql.NullTime
			EventVenue      sql.NullString
			OrganizationID  string
		}
		var batchID string
		if err := rows.Scan(
			&batchID,
			&b.Name,
			&b.Type,
			&b.Price,
			&b.QuantityTotal,
			&b.QuantitySold,
			&b.MaxPurchase,
			&b.EventTitle,
			&b.EventStartDate,
			&b.EventEndDate,
			&b.EventVenue,
			&b.OrganizationID,
		); err != nil {
			return nil, fmt.Errorf("scan lote: %w", err)
		}
		batchMap[batchID] = b
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows lote: %w", err)
	}

	if len(batchMap) != len(batchIDs) {
		return nil, fmt.Errorf("um ou mais lotes não encontrados ou indisponíveis")
	}

	// 2. Validate availability per batch
	requestedPerBatch := make(map[string]int)
	for _, item := range items {
		requestedPerBatch[item.LotID] += item.Quantity
	}

	for batchID, requested := range requestedPerBatch {
		b := batchMap[batchID]
		available := b.QuantityTotal - b.QuantitySold
		if requested > available {
			return nil, fmt.Errorf("lote '%s' tem apenas %d ingresso(s) disponível(is), solicitados: %d", b.Name, available, requested)
		}
		if requested > b.MaxPurchase {
			return nil, fmt.Errorf("lote '%s' permite no máximo %d ingresso(s) por emissão", b.Name, b.MaxPurchase)
		}
	}

	// 3. Process each recipient
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("iniciar transação: %w", err)
	}
	defer tx.Rollback()

	// Create promoter order (type = 'promoter', status = 'paid')
	orderID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (
			id, event_id, user_id, coupon_id,
			total_amount, discount_amount, platform_fee_amount, net_amount,
			status, payment_method, order_type, is_promoter_emission, created_at
		) VALUES ($1, $2, $3, NULL, 0, 0, 0, 0, 'paid', 'promoter', 'promoter', true, NOW())
	`, orderID, eventID, promoterUserID)
	if err != nil {
		return nil, fmt.Errorf("criar pedido promoter: %w", err)
	}

	// Create distribution record
	distributionID := uuid.New().String()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO promoter_distributions (
			id, organization_id, event_id, promoter_user_id, batch_id, 
			recipient_user_id, quantity, recipient_cpf, recipient_email, 
			recipient_phone, recipient_name, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
	`, distributionID, organizationID, eventID, promoterUserID, batchIDs[0], "", 0, "", "", "", "")
	if err != nil {
		return nil, fmt.Errorf("criar distribuição: %w", err)
	}

	var emittedTickets []EmittedTicket
	eventTitle := ""
	var eventStartDate sql.NullTime
	var eventVenue sql.NullString

	for _, item := range items {
		batch := batchMap[item.LotID]
		eventTitle = batch.EventTitle
		eventStartDate = batch.EventStartDate
		eventVenue = batch.EventVenue

		// 3.1 Find or create recipient user by CPF
		cpfDigits := cleanCPF(item.CPF)
		if len(cpfDigits) != 11 {
			return nil, fmt.Errorf("CPF inválido: %s", item.CPF)
		}

		var recipientID string
		var recipientName, recipientEmail, recipientPhone sql.NullString
		var isGuest bool

		// Try to find existing user by CPF
		err = tx.QueryRowContext(ctx, `
			SELECT id, COALESCE(full_name, ''), COALESCE(email, ''), COALESCE(phone, ''), COALESCE(is_guest, false)
			FROM users
			WHERE cpf_digits = $1
		`, cpfDigits).Scan(&recipientID, &recipientName, &recipientEmail, &recipientPhone, &isGuest)

		if err == sql.ErrNoRows {
			// Create guest user
			name := strings.TrimSpace(item.FullName)
			if name == "" {
				name = "Visitante"
			}

			email := sql.NullString{}
			if strings.TrimSpace(item.Email) != "" {
				email = sql.NullString{String: strings.ToLower(strings.TrimSpace(item.Email)), Valid: true}
			}

			phone := sql.NullString{}
			if cleaned := cleanPhone(item.Phone); cleaned != "" {
				phone = sql.NullString{String: cleaned, Valid: true}
			}

			cpfNull := sql.NullString{String: cpfDigits, Valid: true}

			err = tx.QueryRowContext(ctx, `
				INSERT INTO users (email, full_name, cpf, phone, is_guest)
				VALUES ($1, $2, $3, $4, true)
				RETURNING id
			`, email, name, cpfNull, phone).Scan(&recipientID)
			if err != nil {
				return nil, fmt.Errorf("criar usuário guest: %w", err)
			}

			recipientName = sql.NullString{String: name, Valid: true}
			recipientEmail = email
			recipientPhone = phone
			isGuest = true
		} else if err != nil {
			return nil, fmt.Errorf("buscar usuário por CPF: %w", err)
		} else {
			// User exists - update missing fields if provided
			updates := []string{}
			args := []interface{}{}
			argIdx := 1

			if strings.TrimSpace(item.FullName) != "" && (!recipientName.Valid || recipientName.String == "") {
				updates = append(updates, fmt.Sprintf("full_name = $%d", argIdx))
				args = append(args, strings.TrimSpace(item.FullName))
				argIdx++
			}
			if strings.TrimSpace(item.Email) != "" && (!recipientEmail.Valid || recipientEmail.String == "") {
				updates = append(updates, fmt.Sprintf("email = $%d", argIdx))
				args = append(args, strings.ToLower(strings.TrimSpace(item.Email)))
				argIdx++
			}
			if cleaned := cleanPhone(item.Phone); cleaned != "" && (!recipientPhone.Valid || recipientPhone.String == "") {
				updates = append(updates, fmt.Sprintf("phone = $%d", argIdx))
				args = append(args, cleaned)
				argIdx++
			}

			if len(updates) > 0 {
				updates = append(updates, fmt.Sprintf("updated_at = $%d", argIdx))
				args = append(args, time.Now())
				argIdx++
				args = append(args, recipientID)

				query := fmt.Sprintf("UPDATE users SET %s WHERE id = $%d", strings.Join(updates, ", "), argIdx)
				_, err = tx.ExecContext(ctx, query, args...)
				if err != nil {
					log.Printf("PromoterEmit update user: %v", err)
				}
			}
		}

		// 3.2 Create tickets
		for q := 0; q < item.Quantity; q++ {
			qrCode, err := orderservice.GenerateQRCode()
			if err != nil {
				return nil, fmt.Errorf("gerar QR code: %w", err)
			}

			ticketID := uuid.New().String()
			_, err = tx.ExecContext(ctx, `
				INSERT INTO tickets (id, order_id, batch_id, user_id, qr_code, status, source, created_at)
				VALUES ($1, $2, $3, $4, $5, 'valid', 'promoter', NOW())
			`, ticketID, orderID, item.LotID, recipientID, qrCode)
			if err != nil {
				return nil, fmt.Errorf("criar ingresso: %w", err)
			}

			// Update batch quantity_sold
			_, err = tx.ExecContext(ctx, `
				UPDATE ticket_batches SET quantity_sold = quantity_sold + 1 WHERE id = $1
			`, item.LotID)
			if err != nil {
				return nil, fmt.Errorf("atualizar quantidade vendida: %w", err)
			}

			// Add distribution record for this ticket
			_, err = tx.ExecContext(ctx, `
				INSERT INTO promoter_distributions (
					id, organization_id, event_id, promoter_user_id, batch_id,
					recipient_user_id, quantity, recipient_cpf, recipient_email,
					recipient_phone, recipient_name, created_at
				) VALUES ($1, $2, $3, $4, $5, $6, 1, $7, $8, $9, $10, NOW())
			`, uuid.New().String(), organizationID, eventID, promoterUserID, item.LotID,
				recipientID, cpfDigits,
				sql.NullString{String: item.Email, Valid: item.Email != ""},
				sql.NullString{String: cleanPhone(item.Phone), Valid: cleanPhone(item.Phone) != ""},
				sql.NullString{String: item.FullName, Valid: item.FullName != ""},
			)
			if err != nil {
				return nil, fmt.Errorf("criar distribuição detalhada: %w", err)
			}

			emittedTickets = append(emittedTickets, EmittedTicket{
				TicketID:        ticketID,
				QRCode:          qrCode,
				RecipientUserID: recipientID,
				RecipientName:   recipientName.String,
				RecipientEmail:  recipientEmail.String,
				BatchName:       batch.Name,
				BatchType:       batch.Type,
			})
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transação: %w", err)
	}

	// 4. Send emails (async, non-blocking)
	if emailSender != nil && len(emittedTickets) > 0 && eventTitle != "" {
		go func() {
			for _, t := range emittedTickets {
				if t.RecipientEmail == "" {
					continue
				}

				eventData := ""
				eventHora := ""
				eventLocal := ""
				if eventStartDate.Valid {
					loc, _ := time.LoadLocation("America/Sao_Paulo")
					evTime := eventStartDate.Time.In(loc)
					meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
					eventData = fmt.Sprintf("%d %s", evTime.Day(), meses[evTime.Month()])
					if evTime.Minute() == 0 {
						eventHora = fmt.Sprintf("%dh", evTime.Hour())
					} else {
						eventHora = fmt.Sprintf("%dh%02d", evTime.Hour(), evTime.Minute())
					}
				}
				if eventVenue.Valid {
					eventLocal = eventVenue.String
				}

				subject, html, err := emailtemplates.BuildPromoterEmission(emailtemplates.PromoterEmissionData{
					NomeUsuario:  t.RecipientName,
					EventoNome:   eventTitle,
					EventoData:   eventData,
					EventoHora:   eventHora,
					EventoLocal:  eventLocal,
					LoteNome:     t.BatchName,
					QRCode:       t.QRCode,
					PromoterName: "", // Could fetch promoter name if needed
				})
				if err != nil {
					log.Printf("PromoterEmit build email: %v", err)
					continue
				}

				_, err = emailSender.Send(emailsender.Message{
					From:     "Reppy <no-reply@reppy.com.br>",
					To:       []emailsender.Recipient{{Name: t.RecipientName, Email: t.RecipientEmail}},
					Subject:  subject,
					HTMLBody: html,
				})
				if err != nil {
					log.Printf("PromoterEmit send email to %s: %v", t.RecipientEmail, err)
				}
			}
		}()
	}

	return &EmitResult{
		OrderID:         orderID,
		EmittedTickets:  emittedTickets,
		DistributionID:  distributionID,
	}, nil
}