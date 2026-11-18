package refundservice

import (
	"database/sql"
	"fmt"

	"bilheteria-api/services/paymentservice"
)

type TxidRefunder interface {
	RefundByTxid(txid, reason string) error
}

func RefundOrder(db *sql.DB, orderID, reason string) error {
	if paymentservice.Default == nil {
		return fmt.Errorf("gateway de pagamento não inicializado")
	}

	var e2eID, txid string
	err := db.QueryRow(`
		SELECT COALESCE(pix_e2e_id, ''), COALESCE(pix_external_id, '')
		FROM orders WHERE id = $1
	`, orderID).Scan(&e2eID, &txid)
	if err != nil {
		return fmt.Errorf("buscar dados do pix: %w", err)
	}

	if e2eID != "" {
		return paymentservice.Default.Refund(e2eID, reason)
	}
	if txid != "" {
		if refunder, ok := paymentservice.Default.(TxidRefunder); ok {
			return refunder.RefundByTxid(txid, reason)
		}
	}
	return fmt.Errorf("pedido %s sem e2eid do Pix; estorno manual necessário", orderID)
}
