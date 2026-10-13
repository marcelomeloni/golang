package payoutservice

import (
	"database/sql"
	"time"
)

// EscrowReleaseDelay é o prazo (após o término do evento) para liberar o valor
// líquido das vendas de ingresso (venda primária) para a organização.
const EscrowReleaseDelay = 7 * 24 * time.Hour

// primaryOrderFilter exclui pedidos que não são venda primária de ingresso
// (Market de revenda e emissões de promoter não entram no saldo da org).
const primaryOrderFilter = `COALESCE(o.order_type, '') NOT IN ('market', 'promoter')`

// EventEscrow descreve o saldo de um evento e quando ele libera.
type EventEscrow struct {
	EventID   string    `json:"event_id"`
	Title     string    `json:"title"`
	NetAmount float64   `json:"net_amount"`
	ReleaseAt time.Time `json:"release_at"`
	Released  bool      `json:"released"`
	DaysLeft  float64   `json:"days_left"`
}

// OrgBalanceResult é o extrato de saldo de uma organização.
type OrgBalanceResult struct {
	Released  float64       `json:"released"`
	InEscrow  float64       `json:"in_escrow"`
	Reserved  float64       `json:"reserved"`
	Available float64       `json:"available"`
	Events     []EventEscrow `json:"events"`
}

// OrgBalance calcula o saldo da organização a partir dos pedidos pagos.
//   - released: soma de net_amount de pedidos de eventos já liberados
//     (release_at <= agora)
//   - in_escrow: soma de net_amount de pedidos ainda dentro do prazo
//   - reserved: pedidos de repasse pendentes ou já pagos (para não sacar 2x)
//   - available: released - reserved
func OrgBalance(db *sql.DB, orgID string) (OrgBalanceResult, error) {
	var res OrgBalanceResult

	rows, err := db.Query(`
		SELECT
			e.id,
			e.title,
			(COALESCE(e.end_date, e.start_date) + interval '7 days') AS release_at,
			COALESCE(SUM(o.net_amount), 0) AS net
		FROM events e
		JOIN orders o ON o.event_id = e.id
		WHERE e.organization_id = $1
		  AND o.status = 'paid'
		  AND `+primaryOrderFilter+`
		GROUP BY e.id, e.title, release_at
		ORDER BY release_at ASC`, orgID,
	)
	if err != nil {
		return res, err
	}
	defer rows.Close()

	now := time.Now()
	res.Events = []EventEscrow{}

	for rows.Next() {
		var (
			ev        EventEscrow
			releaseAt sql.NullTime
		)
		if err := rows.Scan(&ev.EventID, &ev.Title, &releaseAt, &ev.NetAmount); err != nil {
			return res, err
		}
		if ev.NetAmount <= 0 {
			continue
		}

		if releaseAt.Valid {
			ev.ReleaseAt = releaseAt.Time
			ev.Released = !releaseAt.Time.After(now)
			ev.DaysLeft = releaseAt.Time.Sub(now).Hours() / 24
			if ev.DaysLeft < 0 {
				ev.DaysLeft = 0
			}
		} else {
			// Evento sem data: libera imediatamente.
			ev.ReleaseAt = now
			ev.Released = true
			ev.DaysLeft = 0
		}

		if ev.Released {
			res.Released += ev.NetAmount
		} else {
			res.InEscrow += ev.NetAmount
		}
		res.Events = append(res.Events, ev)
	}
	if err := rows.Err(); err != nil {
		return res, err
	}

	if err := db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM payout_requests
		WHERE organization_id = $1 AND status IN ('pending', 'paid')`, orgID,
	).Scan(&res.Reserved); err != nil {
		return res, err
	}

	res.Available = res.Released - res.Reserved
	if res.Available < 0 {
		res.Available = 0
	}
	return res, nil
}

// OrgMetrics é o resumo financeiro de uma organização para o painel admin.
type OrgMetrics struct {
	OrgID         string  `json:"org_id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	FeePercentage float64 `json:"fee_percentage"`
	Gross         float64 `json:"gross"`
	PlatformFee   float64 `json:"platform_fee"`
	Net           float64 `json:"net"`
	Orders        int     `json:"orders"`
	Released      float64 `json:"released"`
	InEscrow      float64 `json:"in_escrow"`
	Available     float64 `json:"available"`
}

// ListOrgMetrics devolve, por organização, o quanto vendemos, nossa taxa, o
// líquido da org e o saldo em escrow/disponível.
func ListOrgMetrics(db *sql.DB) ([]OrgMetrics, error) {
	rows, err := db.Query(`
		SELECT
			o.id, o.name, o.slug,
			COALESCE(o.platform_fee_percentage, 8.00) AS fee_pct,
			COALESCE(m.gross, 0), COALESCE(m.fees, 0), COALESCE(m.net, 0), COALESCE(m.orders, 0),
			COALESCE(esc.released, 0), COALESCE(esc.in_escrow, 0),
			COALESCE(pr.reserved, 0)
		FROM organizations o
		LEFT JOIN (
			SELECT e.organization_id AS org_id,
			       SUM(o.total_amount)        AS gross,
			       SUM(o.platform_fee_amount) AS fees,
			       SUM(o.net_amount)          AS net,
			       COUNT(*)                   AS orders
			FROM orders o
			JOIN events e ON e.id = o.event_id
			WHERE o.status = 'paid' AND `+primaryOrderFilter+`
			GROUP BY e.organization_id
		) m ON m.org_id = o.id
		LEFT JOIN (
			SELECT e.organization_id AS org_id,
			       SUM(o.net_amount) FILTER (
			           WHERE (COALESCE(e.end_date, e.start_date) + interval '7 days') <= NOW()
			       ) AS released,
			       SUM(o.net_amount) FILTER (
			           WHERE (COALESCE(e.end_date, e.start_date) + interval '7 days') > NOW()
			       ) AS in_escrow
			FROM orders o
			JOIN events e ON e.id = o.event_id
			WHERE o.status = 'paid' AND `+primaryOrderFilter+`
			GROUP BY e.organization_id
		) esc ON esc.org_id = o.id
		LEFT JOIN (
			SELECT organization_id AS org_id, SUM(amount) AS reserved
			FROM payout_requests
			WHERE status IN ('pending', 'paid')
			GROUP BY organization_id
		) pr ON pr.org_id = o.id
		ORDER BY COALESCE(m.fees, 0) DESC, o.name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []OrgMetrics{}
	for rows.Next() {
		var m OrgMetrics
		var reserved float64
		if err := rows.Scan(
			&m.OrgID, &m.Name, &m.Slug, &m.FeePercentage,
			&m.Gross, &m.PlatformFee, &m.Net, &m.Orders,
			&m.Released, &m.InEscrow, &reserved,
		); err != nil {
			return nil, err
		}
		m.Available = m.Released - reserved
		if m.Available < 0 {
			m.Available = 0
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// LoadOrgMetrics devolve as métricas de uma única organização.
func LoadOrgMetrics(db *sql.DB, orgID string) (*OrgMetrics, error) {
	var m OrgMetrics
	var reserved float64
	err := db.QueryRow(`
		SELECT
			o.id, o.name, o.slug,
			COALESCE(o.platform_fee_percentage, 8.00) AS fee_pct,
			COALESCE(m.gross, 0), COALESCE(m.fees, 0), COALESCE(m.net, 0), COALESCE(m.orders, 0),
			COALESCE(esc.released, 0), COALESCE(esc.in_escrow, 0),
			COALESCE(pr.reserved, 0)
		FROM organizations o
		LEFT JOIN (
			SELECT e.organization_id AS org_id,
			       SUM(o.total_amount)        AS gross,
			       SUM(o.platform_fee_amount) AS fees,
			       SUM(o.net_amount)          AS net,
			       COUNT(1)                   AS orders
			FROM events e
			JOIN orders o ON o.event_id = e.id
			WHERE o.status = 'paid' AND `+primaryOrderFilter+`
			GROUP BY e.organization_id
		) m ON m.org_id = o.id
		LEFT JOIN (
			SELECT e.organization_id AS org_id,
			       SUM(o.net_amount) FILTER (WHERE (COALESCE(e.end_date, e.start_date) + interval '7 days') <= NOW()) AS released,
			       SUM(o.net_amount) FILTER (WHERE (COALESCE(e.end_date, e.start_date) + interval '7 days') > NOW())  AS in_escrow
			FROM events e
			JOIN orders o ON o.event_id = e.id
			WHERE o.status = 'paid' AND `+primaryOrderFilter+`
			GROUP BY e.organization_id
		) esc ON esc.org_id = o.id
		LEFT JOIN (
			SELECT organization_id AS org_id, SUM(amount) AS reserved
			FROM payout_requests
			WHERE status IN ('pending', 'paid')
			GROUP BY organization_id
		) pr ON pr.org_id = o.id
		WHERE o.id = $1`, orgID,
	).Scan(
		&m.OrgID, &m.Name, &m.Slug, &m.FeePercentage,
		&m.Gross, &m.PlatformFee, &m.Net, &m.Orders,
		&m.Released, &m.InEscrow, &reserved,
	)
	if err != nil {
		return nil, err
	}
	m.Available = m.Released - reserved
	if m.Available < 0 {
		m.Available = 0
	}
	return &m, nil
}

// EventMetrics é o resumo financeiro de um evento (venda primária).
type EventMetrics struct {
	EventID     string    `json:"event_id"`
	OrgID       string    `json:"org_id"`
	OrgName     string    `json:"org_name"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Gross       float64   `json:"gross"`
	PlatformFee float64   `json:"platform_fee"`
	Net         float64   `json:"net"`
	Orders      int       `json:"orders"`
	ReleaseAt   time.Time `json:"release_at"`
	Released    bool      `json:"released"`
	DaysLeft    float64   `json:"days_left"`
}

// EventMetricsForOrg lista os eventos de uma organização com os números.
func EventMetricsForOrg(db *sql.DB, orgID string) ([]EventMetrics, error) {
	rows, err := db.Query(`
		SELECT
			e.id, e.organization_id, o.name, e.title, e.status,
			COALESCE(e.start_date, NOW()), COALESCE(e.end_date, e.start_date, NOW()),
			(COALESCE(e.end_date, e.start_date) + interval '7 days') AS release_at,
			COALESCE(SUM(ord.total_amount)        FILTER (WHERE ord.status = 'paid'), 0) AS gross,
			COALESCE(SUM(ord.platform_fee_amount) FILTER (WHERE ord.status = 'paid'), 0) AS fees,
			COALESCE(SUM(ord.net_amount)          FILTER (WHERE ord.status = 'paid'), 0) AS net,
			COUNT(ord.id) FILTER (WHERE ord.status = 'paid') AS orders
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		LEFT JOIN orders ord ON ord.event_id = e.id AND COALESCE(ord.order_type, '') NOT IN ('market', 'promoter')
		WHERE e.organization_id = $1
		GROUP BY e.id, e.organization_id, o.name, e.title, e.status, e.start_date, e.end_date
		ORDER BY e.start_date DESC`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []EventMetrics{}
	now := time.Now()
	for rows.Next() {
		var (
			m         EventMetrics
			releaseAt sql.NullTime
		)
		if err := rows.Scan(
			&m.EventID, &m.OrgID, &m.OrgName, &m.Title, &m.Status,
			&m.StartDate, &m.EndDate, &releaseAt,
			&m.Gross, &m.PlatformFee, &m.Net, &m.Orders,
		); err != nil {
			return nil, err
		}
		fillRelease(&m, releaseAt, now)
		list = append(list, m)
	}
	return list, rows.Err()
}

// EventMetricsByID devolve o resumo financeiro de um único evento.
func EventMetricsByID(db *sql.DB, eventID string) (EventMetrics, error) {
	var m EventMetrics
	var releaseAt sql.NullTime
	err := db.QueryRow(`
		SELECT
			e.id, e.organization_id, o.name, e.title, e.status,
			COALESCE(e.start_date, NOW()), COALESCE(e.end_date, e.start_date, NOW()),
			(COALESCE(e.end_date, e.start_date) + interval '7 days') AS release_at,
			COALESCE(SUM(ord.total_amount)        FILTER (WHERE ord.status = 'paid'), 0) AS gross,
			COALESCE(SUM(ord.platform_fee_amount) FILTER (WHERE ord.status = 'paid'), 0) AS fees,
			COALESCE(SUM(ord.net_amount)          FILTER (WHERE ord.status = 'paid'), 0) AS net,
			COUNT(ord.id) FILTER (WHERE ord.status = 'paid') AS orders
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		LEFT JOIN orders ord ON ord.event_id = e.id AND COALESCE(ord.order_type, '') NOT IN ('market', 'promoter')
		WHERE e.id = $1
		GROUP BY e.id, e.organization_id, o.name, e.title, e.status, e.start_date, e.end_date`, eventID,
	).Scan(
		&m.EventID, &m.OrgID, &m.OrgName, &m.Title, &m.Status,
		&m.StartDate, &m.EndDate, &releaseAt,
		&m.Gross, &m.PlatformFee, &m.Net, &m.Orders,
	)
	if err != nil {
		return m, err
	}
	fillRelease(&m, releaseAt, time.Now())
	return m, nil
}

func fillRelease(m *EventMetrics, releaseAt sql.NullTime, now time.Time) {
	if releaseAt.Valid {
		m.ReleaseAt = releaseAt.Time
		m.Released = !releaseAt.Time.After(now)
		m.DaysLeft = releaseAt.Time.Sub(now).Hours() / 24
		if m.DaysLeft < 0 {
			m.DaysLeft = 0
		}
	} else {
		m.ReleaseAt = now
		m.Released = true
	}
}

// PlatformMetrics é o total arrecadado pela plataforma (venda primária).
type PlatformMetrics struct {
	Gross        float64 `json:"gross"`
	PlatformFee  float64 `json:"platform_fee"`
	Net          float64 `json:"net"`
	PaidOrders   int     `json:"paid_orders"`
	Organizations int    `json:"organizations"`
	Events       int     `json:"events"`
}

// LoadPlatformMetrics agrega o total de taxas de venda primária da plataforma.
func LoadPlatformMetrics(db *sql.DB) (PlatformMetrics, error) {
	var m PlatformMetrics
	err := db.QueryRow(`
		SELECT
			COALESCE(SUM(o.total_amount), 0),
			COALESCE(SUM(o.platform_fee_amount), 0),
			COALESCE(SUM(o.net_amount), 0),
			COUNT(*)
		FROM orders o
		WHERE o.status = 'paid' AND `+primaryOrderFilter,
	).Scan(&m.Gross, &m.PlatformFee, &m.Net, &m.PaidOrders)
	if err != nil {
		return m, err
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM organizations`).Scan(&m.Organizations)
	_ = db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&m.Events)
	return m, nil
}
