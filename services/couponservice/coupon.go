package couponservice

import (
	"database/sql"
	"strings"
	"time"
)

// Coupon contém os dados de um cupom já validado.
type Coupon struct {
	ID            string
	DiscountType  string
	DiscountValue float64
	MaxUses       sql.NullInt64
	UsedCount     int
	ExpiresAt     sql.NullTime
}

// Load busca e valida um cupom para o evento.
// Retorna (cupom, mensagem de erro para o usuário, erro técnico).
// Se a mensagem de erro for não-vazia, o cupom é inválido mas não é um erro de sistema.
func Load(db *sql.DB, eventID, code string) (*Coupon, string, error) {
	var c Coupon
	err := db.QueryRow(`
		SELECT id, discount_type, discount_value, max_uses, used_count, expires_at
		FROM coupons
		WHERE event_id = $1
		  AND UPPER(code) = UPPER($2)
		  AND active = true`,
		eventID, strings.TrimSpace(code),
	).Scan(&c.ID, &c.DiscountType, &c.DiscountValue, &c.MaxUses, &c.UsedCount, &c.ExpiresAt)

	if err == sql.ErrNoRows {
		return nil, "Cupom inválido ou expirado.", nil
	}
	if err != nil {
		return nil, "", err
	}

	if c.ExpiresAt.Valid && time.Now().After(c.ExpiresAt.Time) {
		return nil, "Esse cupom já expirou.", nil
	}
	if c.MaxUses.Valid && c.UsedCount >= int(c.MaxUses.Int64) {
		return nil, "Esse cupom já atingiu o limite de uso.", nil
	}

	return &c, "", nil
}

// ApplyDiscount calcula o desconto sobre o subtotal em reais.
func ApplyDiscount(subtotal float64, c *Coupon) float64 {
	if c == nil {
		return 0
	}
	switch c.DiscountType {
	case "percentage":
		return subtotal * (c.DiscountValue / 100)
	case "fixed":
		if c.DiscountValue > subtotal {
			return subtotal
		}
		return c.DiscountValue
	}
	return 0
}

// IncrementUsage incrementa o used_count do cupom após a confirmação do pedido.
func IncrementUsage(tx *sql.Tx, couponID string) error {
	_, err := tx.Exec(`UPDATE coupons SET used_count = used_count + 1 WHERE id = $1`, couponID)
	return err
}