package client

import (
	"database/sql"
	"log"
	"time"
)

func groupTickets(rows *sql.Rows) (proximos, passados []MyTicket, err error) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")

	proximos = []MyTicket{}
	passados = []MyTicket{}

	for rows.Next() {
		t, scanErr := scanTicketRow(rows.Scan, loc)
		if scanErr != nil {
			log.Printf("groupTickets scan: %v", scanErr)
			continue
		}

		if t.Status == "ativo" {
			proximos = append(proximos, t)
		} else {
			passados = append(passados, t)
		}
	}

	return proximos, passados, rows.Err()
}

func scanSingleTicket(row *sql.Row) (MyTicket, error) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	return scanTicketRow(row.Scan, loc)
}

func scanTicketRow(scan func(...any) error, loc *time.Location) (MyTicket, error) {
	var (
		id                string
		eventID           string
		qrCode            string
		dbStatus          string
		checkedInAt       sql.NullTime
		loteName          sql.NullString
		ticketPrice       float64
		allowTransfer     sql.NullBool
		slug              string
		title             string
		imageURL          sql.NullString
		startDate         sql.NullTime
		endDate           sql.NullTime
		locationJSON      []byte
		allowReppyMarket  bool
		currentBatchPrice sql.NullFloat64
		isListed          bool
		listingID         sql.NullString
		listingPrice      sql.NullFloat64
	)

	err := scan(
		&id, &eventID, &qrCode, &dbStatus, &checkedInAt,
		&loteName, &ticketPrice, &allowTransfer,
		&slug, &title, &imageURL,
		&startDate, &endDate, &locationJSON,
		&allowReppyMarket,
		&currentBatchPrice,
		&isListed,
		&listingID,
		&listingPrice,
	)
	if err != nil {
		return MyTicket{}, err
	}

	addr := parseLocation(locationJSON)

	var batchPricePtr *float64
	if currentBatchPrice.Valid && currentBatchPrice.Float64 > 0 {
		v := currentBatchPrice.Float64
		batchPricePtr = &v
	}

	var listingIDPtr *string
	if listingID.Valid {
		v := listingID.String
		listingIDPtr = &v
	}

	var listingPricePtr *float64
	if listingPrice.Valid && listingPrice.Float64 > 0 {
		v := listingPrice.Float64
		listingPricePtr = &v
	}

	return MyTicket{
		ID:                id,
		EventID:           eventID,
		Status:            resolveClientStatus(dbStatus, checkedInAt, endDate, loc),
		QRCode:            qrCode,
		LoteName:          loteName.String,
		TicketPrice:       ticketPrice,
		AllowTransfer:     allowTransfer.Bool,
		AllowReppyMarket:  allowReppyMarket,
		CurrentBatchPrice: batchPricePtr,
		IsListed:          isListed,
		ListingID:         listingIDPtr,
		ListingPrice:      listingPricePtr,
		Evento: MyTicketEvent{
			Slug:         slug,
			Nome:         title,
			Data:         formatDate(startDate, loc),
			Hora:         formatTime(startDate, loc),
			VenueName:    addr.VenueName,
			Street:       addr.Street,
			Number:       addr.Number,
			Neighborhood: addr.Neighborhood,
			City:         addr.City,
			State:        addr.State,
			CEP:          addr.CEP,
			Local:        formatVenueShort(addr),
			ImageURL:     imageURL.String,
		},
	}, nil
}