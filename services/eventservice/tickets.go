package eventservice

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ─── Tipos de input (compartilhados com os controllers) ───────────────────────

type TicketCategoryInput struct {
	Name           string           `json:"name"`
	Type           string           `json:"type"`           // paid | free
	Description    string           `json:"description"`
	Availability   string           `json:"availability"`   // public | hidden | guestlist
	IsTransferable bool             `json:"is_transferable"`
	InReppyMarket  bool             `json:"in_reppy_market"`
	Lots           []TicketLotInput `json:"lots"`
}

type TicketLotInput struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Price        float64 `json:"price"`
	Quantity     int     `json:"quantity"`
	SalesTrigger string  `json:"sales_trigger"`  // date | batch
	TriggerLotID string  `json:"trigger_lot_id"`
	FeePayer     string  `json:"fee_payer"`      // customer | organizer
	MinPurchase  int     `json:"min_purchase"`
	MaxPurchase  int     `json:"max_purchase"`
	SalesStart   string  `json:"sales_start"`
	SalesEnd     string  `json:"sales_end"`
}

// InsertTicketCategories persiste as categorias e seus lotes dentro de uma transação aberta.
// Cada categoria gera uma linha em ticket_categories.
// Cada lote gera uma linha em ticket_batches com o category_id correto.
func InsertTicketCategories(ctx context.Context, tx *sql.Tx, eventID string, categories []TicketCategoryInput) error {
	for position, cat := range categories {
		categoryID := uuid.New().String()
		lotType := resolveLotType(cat.Type)
		availability := MapAvailability(cat.Availability)

		if err := insertCategory(ctx, tx, categoryID, eventID, cat, lotType, availability, position); err != nil {
			return fmt.Errorf("categoria '%s': %w", cat.Name, err)
		}

		if err := insertLots(ctx, tx, eventID, categoryID, lotType, cat.Lots); err != nil {
			return err
		}
	}
	return nil
}

func insertCategory(
	ctx context.Context, tx *sql.Tx,
	categoryID, eventID string,
	cat TicketCategoryInput,
	lotType, availability string,
	position int,
) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO ticket_categories
		   (id, event_id, name, type, description, availability,
		    is_transferable, in_reppy_market, position)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		categoryID, eventID,
		cat.Name, lotType,
		nullableText(cat.Description),
		availability,
		cat.IsTransferable, cat.InReppyMarket,
		position,
	)
	return err
}

func insertLots(ctx context.Context, tx *sql.Tx, eventID, categoryID, lotType string, lots []TicketLotInput) error {
	// id do lote (cliente) → batchID para resolver referências de lote-gatilho
	lotIDToBatchID := make(map[string]string, len(lots))
	// nome → batchID (fallback para payloads antigos sem id)
	lotNameToBatchID := make(map[string]string, len(lots))

	for i, lot := range lots {
		batchID := uuid.New().String()
		prevBatchID := resolvePreviousBatch(lot, lots[:i], lotIDToBatchID, lotNameToBatchID)

		if err := insertBatch(ctx, tx, batchID, eventID, categoryID, lotType, prevBatchID, lot); err != nil {
			return fmt.Errorf("lote '%s': %w", lot.Name, err)
		}

		if lot.ID != "" {
			lotIDToBatchID[lot.ID] = batchID
		}
		lotNameToBatchID[lot.Name] = batchID
	}
	return nil
}

func insertBatch(
	ctx context.Context, tx *sql.Tx,
	batchID, eventID, categoryID, lotType string,
	prevBatchID *string,
	lot TicketLotInput,
) error {
	salesTrigger := MapSalesTrigger(lot.SalesTrigger, prevBatchID)
	feePayer := MapFeePayer(lot.FeePayer)

	minP := defaultInt(lot.MinPurchase, 1)
	maxP := defaultInt(lot.MaxPurchase, 10)

	salesStart := parseTime(lot.SalesStart)
	salesEnd := parseTime(lot.SalesEnd)

	_, err := tx.ExecContext(ctx,
		`INSERT INTO ticket_batches
		   (id, event_id, category_id, previous_batch_id, name, type,
		    price, quantity_total, status, fee_payer,
		    sales_trigger, allow_transfer, min_purchase, max_purchase,
		    start_date, end_date)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,true,$11,$12,$13,$14)`,
		batchID, eventID, categoryID, prevBatchID,
		lot.Name, lotType,
		lot.Price, lot.Quantity,
		feePayer,
		salesTrigger,
		minP, maxP,
		salesStart, salesEnd,
	)
	return err
}

// resolvePreviousBatch retorna o batchID do lote-gatilho quando o lote usa
// virada automática (salesTrigger = "batch"). A resolução é feita pelo id do
// lote enviado pelo frontend (estável dentro do mesmo payload) e cai no nome
// como fallback para payloads legados.
func resolvePreviousBatch(lot TicketLotInput, prevLots []TicketLotInput, idMap, nameMap map[string]string) *string {
	if !isBatchTrigger(lot.SalesTrigger) {
		return nil
	}
	// Tenta resolver pelo ID explícito enviado pelo frontend
	if lot.TriggerLotID != "" {
		if id, ok := idMap[lot.TriggerLotID]; ok {
			return &id
		}
		if id, ok := nameMap[lot.TriggerLotID]; ok {
			return &id
		}
	}
	// Fallback: lote imediatamente anterior na mesma categoria
	if len(prevLots) > 0 {
		prev := prevLots[len(prevLots)-1]
		if prev.ID != "" {
			if id, ok := idMap[prev.ID]; ok {
				return &id
			}
		}
		if id, ok := nameMap[prev.Name]; ok {
			return &id
		}
	}
	return nil
}

// isBatchTrigger aceita tanto o valor do formulário ("batch") quanto o valor
// já persistido no banco ("previous_batch_sold_out"), evitando que o gatilho
// seja rebaixado para "date" em um round-trip de edição.
func isBatchTrigger(v string) bool {
	return v == "batch" || v == "previous_batch_sold_out"
}

// ─── Helpers privados ────────────────────────────────────────────────────────

func resolveLotType(t string) string {
	if t == "free" {
		return "free"
	}
	return "paid"
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func defaultInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func nullableText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}