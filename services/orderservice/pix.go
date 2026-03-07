package orderservice

import (
	"database/sql"
	"fmt"
)

// SavePixCharge persiste o ID externo da cobrança Pix na ordem.
// Assume que a coluna `pix_external_id` existe na tabela orders.
func SavePixCharge(db *sql.DB, orderID, externalID string) error {
	_, err := db.Exec(
		`UPDATE orders SET pix_external_id = $1 WHERE id = $2`,
		externalID, orderID,
	)
	if err != nil {
		return fmt.Errorf("salvar pix_external_id: %w", err)
	}
	return nil
}

// GetPixExternalID retorna o ID externo da cobrança Pix vinculada ao pedido.
func GetPixExternalID(db *sql.DB, orderID string) (string, error) {
	var externalID sql.NullString
	err := db.QueryRow(
		`SELECT pix_external_id FROM orders WHERE id = $1`,
		orderID,
	).Scan(&externalID)
	if err != nil {
		return "", fmt.Errorf("buscar pix_external_id: %w", err)
	}
	if !externalID.Valid || externalID.String == "" {
		return "", fmt.Errorf("pix_external_id não encontrado para order %s", orderID)
	}
	return externalID.String, nil
}