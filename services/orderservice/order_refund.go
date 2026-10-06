package orderservice

import (
	"database/sql"
	"fmt"
	"log"
)

// MarkOrderRefunded confirma o reembolso de um pedido no banco:
//  1. orders.status -> 'refunded'
//  2. tickets do pedido -> 'cancelled'
//  3. transação de mercado aberta -> 'cancelled' e listing reaberto
//
// Deve ser chamado depois que o estorno foi confirmado na AbacatePay (via API
// ou via webhook).
func MarkOrderRefunded(db *sql.DB, orderID string, markRefundsCompleted bool) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("MarkOrderRefunded: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE orders SET status = 'refunded', updated_at = NOW() WHERE id = $1
	`, orderID); err != nil {
		return fmt.Errorf("MarkOrderRefunded: update order: %w", err)
	}

	if _, err := tx.Exec(`
		UPDATE tickets SET status = 'cancelled' WHERE order_id = $1
	`, orderID); err != nil {
		return fmt.Errorf("MarkOrderRefunded: update tickets: %w", err)
	}

	if markRefundsCompleted {
		if _, err := tx.Exec(`
			UPDATE refunds SET status = 'completed'
			WHERE order_id = $1 AND status IN ('pending', 'refunding')
		`, orderID); err != nil {
			return fmt.Errorf("MarkOrderRefunded: update refunds: %w", err)
		}
	}

	// Mercado: fecha a transação pendente/hold e reabre o listing (o ingresso
	// volta pro vendedor).
	var listingID string
	err = tx.QueryRow(`
		SELECT listing_id FROM market_transactions
		WHERE order_id = $1 AND escrow_status IN ('pending', 'held')
		LIMIT 1
	`, orderID).Scan(&listingID)
	if err == nil {
		if _, err := tx.Exec(`
			UPDATE market_listings SET status = 'active', updated_at = NOW() WHERE id = $1
		`, listingID); err != nil {
			return fmt.Errorf("MarkOrderRefunded: reopen listing: %w", err)
		}
		if _, err := tx.Exec(`
			UPDATE market_transactions SET escrow_status = 'cancelled' WHERE order_id = $1
		`, orderID); err != nil {
			return fmt.Errorf("MarkOrderRefunded: cancel transaction: %w", err)
		}
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("MarkOrderRefunded: market transaction: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("MarkOrderRefunded: commit: %w", err)
	}

	log.Printf("MarkOrderRefunded: orderID=%s reembolsado ✓", orderID)
	return nil
}