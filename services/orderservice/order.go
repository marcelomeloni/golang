package orderservice

import (
	"database/sql"
	"fmt"
	"strings"

	"bilheteria-api/services/couponservice"
)

type GuestInfo struct {
	Name  string
	Email string
	CPF   string
}

// BatchInfo contém os dados de um lote de ingressos.
type BatchInfo struct {
	ID            string
	Price         float64
	FeePayer      string
	Type          string // "paid" | "free" | "donation"
	Status        string
	QuantityTotal int
	QuantitySold  int
	MinPurchase   int
	MaxPurchase   int
}

// OrderItem é o item canônico do pedido (já validado).
type OrderItem struct {
	LotID string
	Qty   int
}

// OrderResult é retornado após a criação do pedido.
type OrderResult struct {
	OrderID       string
	PaymentMethod string
	TotalAmount   float64
	AllFree       bool
}

// ConflictInfo é preenchido quando o guest não pode ser criado por conflito de CPF ou e-mail.
type ConflictInfo struct {
	Code        string // "cpf_already_exists" | "email_already_exists"
	MaskedEmail string
}

// LoadBatches busca os lotes no banco para os IDs informados dentro do evento.
func LoadBatches(db *sql.DB, eventID string, lotIDs []string) (map[string]BatchInfo, error) {
	placeholders := make([]string, len(lotIDs))
	args := make([]interface{}, len(lotIDs)+1)
	args[0] = eventID
	for i, id := range lotIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	rows, err := db.Query(fmt.Sprintf(`
    SELECT id, price, fee_payer, type, status,
           quantity_total, quantity_sold, min_purchase, max_purchase
    FROM ticket_batches
    WHERE event_id = $1
      AND id IN (%s)
      AND status = 'active'
      AND (start_date IS NULL OR start_date <= NOW())
      AND (end_date IS NULL OR end_date > NOW())`,
		strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	batches := make(map[string]BatchInfo)
	for rows.Next() {
		var b BatchInfo
		if err := rows.Scan(&b.ID, &b.Price, &b.FeePayer, &b.Type, &b.Status,
			&b.QuantityTotal, &b.QuantitySold, &b.MinPurchase, &b.MaxPurchase); err != nil {
			return nil, err
		}
		batches[b.ID] = b
	}
	return batches, rows.Err()
}

// ValidateItems verifica disponibilidade e limites de compra para cada item.
// Retorna uma mensagem de erro para o usuário (vazia = ok).
func ValidateItems(batches map[string]BatchInfo, items []OrderItem) string {
	for _, item := range items {
		b, ok := batches[item.LotID]
		if !ok {
			return fmt.Sprintf("lote %s indisponível", item.LotID)
		}
		available := b.QuantityTotal - b.QuantitySold
		if item.Qty > available {
			return fmt.Sprintf("lote '%s' tem apenas %d ingresso(s) disponível(is)", item.LotID, available)
		}
		if item.Qty < b.MinPurchase {
			return fmt.Sprintf("compra mínima para este lote: %d", b.MinPurchase)
		}
		if item.Qty > b.MaxPurchase {
			return fmt.Sprintf("compra máxima para este lote: %d", b.MaxPurchase)
		}
	}
	return ""
}

// CalcSubtotal calcula o subtotal e indica se todos os itens são gratuitos.
func CalcSubtotal(batches map[string]BatchInfo, items []OrderItem) (subtotal float64, allFree bool) {
	allFree = true
	for _, item := range items {
		b := batches[item.LotID]
		subtotal += b.Price * float64(item.Qty)
		if b.Type != "free" {
			allFree = false
		}
	}
	return subtotal, allFree
}

// maskEmail mascara o e-mail para exibição: mar****cs@gm**.com
func maskEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return "****"
	}
	local, domain := parts[0], parts[1]

	maskedLocal := local
	if len(local) > 3 {
		maskedLocal = local[:3] + strings.Repeat("*", len(local)-3)
	}

	domainParts := strings.SplitN(domain, ".", 2)
	maskedDomain := domain
	if len(domainParts) == 2 {
		prefix := domainParts[0]
		suffix := domainParts[1]
		visiblePrefix := 2
		if len(prefix) <= visiblePrefix {
			visiblePrefix = 1
		}
		maskedDomain = prefix[:visiblePrefix] + "**." + suffix
	}

	return maskedLocal + "@" + maskedDomain
}

// ResolveUserID retorna o ID do usuário autenticado ou cria um guest.
// Segundo retorno é um *ConflictInfo preenchido quando CPF ou e-mail já existem.
func ResolveUserID(db *sql.DB, contextUserID string, guest *GuestInfo) (sql.NullString, *ConflictInfo, error) {
	if contextUserID != "" {
		return sql.NullString{String: contextUserID, Valid: true}, nil, nil
	}

	name := "Visitante"
	var email, cpf sql.NullString

	if guest != nil {
		if guest.Name != "" {
			name = guest.Name
		}
		if guest.Email != "" {
			email = sql.NullString{String: guest.Email, Valid: true}
		}
		if guest.CPF != "" {
			cleaned := strings.NewReplacer(".", "", "-", "").Replace(guest.CPF)
			cpf = sql.NullString{String: cleaned, Valid: true}
		}
	}

	var guestID string
	err := db.QueryRow(`
		INSERT INTO users (email, full_name, cpf, is_guest)
		VALUES ($1, $2, $3, true)
		RETURNING id`,
		email, name, cpf,
	).Scan(&guestID)

	if err != nil {
		if strings.Contains(err.Error(), "users_cpf_key") {
			var existingEmail string
			_ = db.QueryRow(`SELECT COALESCE(email, '') FROM users WHERE cpf = $1`, cpf.String).Scan(&existingEmail)
			return sql.NullString{}, &ConflictInfo{
				Code:        "cpf_already_exists",
				MaskedEmail: maskEmail(existingEmail),
			}, nil
		}
		if strings.Contains(err.Error(), "users_email_key") {
			return sql.NullString{}, &ConflictInfo{
				Code:        "email_already_exists",
				MaskedEmail: maskEmail(email.String),
			}, nil
		}
		return sql.NullString{}, nil, fmt.Errorf("criar usuário guest: %w", err)
	}

	return sql.NullString{String: guestID, Valid: true}, nil, nil
}

// Persist salva o pedido e os ingressos dentro de uma transação.
// O trigger check_batch_capacity no banco incrementa quantity_sold automaticamente.
func Persist(
	tx *sql.Tx,
	eventID string,
	userID sql.NullString,
	coupon *couponservice.Coupon,
	items []OrderItem,
	grandTotal, discountAmount, platformFeeAmount float64,
	allFree bool,
) (orderID string, err error) {
	paymentMethod := "pix"
	orderStatus := "pending"
	if allFree || grandTotal == 0 {
		paymentMethod = "manual"
		orderStatus = "paid"
	}

	couponID := sql.NullString{}
	if coupon != nil {
		couponID = sql.NullString{String: coupon.ID, Valid: true}
	}

	err = tx.QueryRow(`
		INSERT INTO orders
		  (event_id, user_id, coupon_id,
		   total_amount, discount_amount, platform_fee_amount, net_amount,
		   status, payment_method)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id`,
		eventID, userID, couponID,
		grandTotal, discountAmount, platformFeeAmount, grandTotal-platformFeeAmount,
		orderStatus, paymentMethod,
	).Scan(&orderID)
	if err != nil {
		return "", fmt.Errorf("inserir order: %w", err)
	}

	for _, item := range items {
		for range item.Qty {
			qr, err := GenerateQRCode()
			if err != nil {
				return "", fmt.Errorf("gerar qr code: %w", err)
			}
			_, err = tx.Exec(`
				INSERT INTO tickets (order_id, batch_id, user_id, qr_code, status)
				VALUES ($1, $2, $3, $4, 'valid')`,
				orderID, item.LotID, userID, qr,
			)
			if err != nil {
				return "", fmt.Errorf("inserir ticket: %w", err)
			}
		}
	}

	return orderID, nil
}