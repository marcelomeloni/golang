package client

type MyTicketEvent struct {
	Slug         string `json:"slug"`
	Nome         string `json:"nome"`
	Data         string `json:"data"`
	Hora         string `json:"hora"`
	VenueName    string `json:"venueName"`
	Street       string `json:"street"`
	Number       string `json:"number"`
	Neighborhood string `json:"neighborhood"`
	City         string `json:"city"`
	State        string `json:"state"`
	CEP          string `json:"cep"`
	Local        string `json:"local"`
	ImageURL     string `json:"imagemUrl"`
}

type MyTicket struct {
	ID                string        `json:"id"`
	EventID           string        `json:"eventId"`
	Status            string        `json:"status"`
	QRCode            string        `json:"qrCode"`
	LoteName          string        `json:"lote"`
	TicketPrice       float64       `json:"ticketPrice"`
	AllowTransfer     bool          `json:"allowTransfer"`
	AllowReppyMarket  bool          `json:"allowReppyMarket"`
	CurrentBatchPrice *float64      `json:"currentBatchPrice"`
	IsListed          bool          `json:"isListed"`
	ListingID         *string       `json:"listingId"`
	ListingPrice      *float64      `json:"listingPrice"`
	Evento            MyTicketEvent `json:"evento"`
}

const myTicketsQuery = `
	SELECT
		t.id,
		o.event_id,
		t.qr_code,
		t.status,
		t.checked_in_at,
		tb.name                               AS lote_name,
		COALESCE(tb.price, 0)                 AS ticket_price,
		tb.allow_transfer,
		e.slug,
		e.title,
		e.image_url,
		e.start_date,
		e.end_date,
		e.location,
		COALESCE(e.allow_reppy_market, false)
			AND COALESCE(tc.in_reppy_market, true)
			AND tb.price > 0                        AS allow_reppy_market,
		(
			SELECT MAX(tb2.price)
			FROM ticket_batches tb2
			WHERE tb2.event_id    = e.id
			  AND tb2.category_id = tb.category_id
			  AND tb2.status      = 'active'
		) AS current_batch_price,
		EXISTS(
			SELECT 1 FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
		) AS is_listed,
		(
			SELECT ml.id FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
			LIMIT 1
		) AS listing_id,
		(
			SELECT ml.price FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
			LIMIT 1
		) AS listing_price
	FROM tickets t
	JOIN orders          o  ON o.id  = t.order_id
	JOIN events          e  ON e.id  = o.event_id
	LEFT JOIN ticket_batches    tb ON tb.id = t.batch_id
	LEFT JOIN ticket_categories tc ON tc.id = tb.category_id
	WHERE t.user_id = $1
	  AND o.status  = 'paid'
	  AND t.status NOT IN ('cancelled')
	ORDER BY e.start_date ASC`

const singleTicketQuery = `
	SELECT
		t.id,
		o.event_id,
		t.qr_code,
		t.status,
		t.checked_in_at,
		tb.name                               AS lote_name,
		COALESCE(tb.price, 0)                 AS ticket_price,
		tb.allow_transfer,
		e.slug,
		e.title,
		e.image_url,
		e.start_date,
		e.end_date,
		e.location,
		COALESCE(e.allow_reppy_market, false)
			AND COALESCE(tc.in_reppy_market, true)
			AND tb.price > 0                        AS allow_reppy_market,
		(
			SELECT MAX(tb2.price)
			FROM ticket_batches tb2
			WHERE tb2.event_id    = e.id
			  AND tb2.category_id = tb.category_id
			  AND tb2.status      = 'active'
		) AS current_batch_price,
		EXISTS(
			SELECT 1 FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
		) AS is_listed,
		(
			SELECT ml.id FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
			LIMIT 1
		) AS listing_id,
		(
			SELECT ml.price FROM market_listings ml
			WHERE ml.ticket_id = t.id AND ml.status = 'active'
			LIMIT 1
		) AS listing_price
	FROM tickets t
	JOIN orders          o  ON o.id  = t.order_id
	JOIN events          e  ON e.id  = o.event_id
	LEFT JOIN ticket_batches    tb ON tb.id = t.batch_id
	LEFT JOIN ticket_categories tc ON tc.id = tb.category_id
	WHERE t.id      = $1
	  AND t.user_id = $2
	  AND o.status  = 'paid'`