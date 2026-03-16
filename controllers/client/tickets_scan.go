package client

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

var (
	diasSemana = []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
	meses      = []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
)

func resolveClientStatus(dbStatus string, checkedInAt, endDate sql.NullTime, loc *time.Location) string {
	if dbStatus == "used" || checkedInAt.Valid {
		return "usado"
	}
	if dbStatus == "transferred" {
		return "encerrado"
	}
	if endDate.Valid && time.Now().In(loc).After(endDate.Time.In(loc)) {
		return "encerrado"
	}
	return "ativo"
}

func calcDaysUntil(t sql.NullTime, loc *time.Location) *int {
	if !t.Valid {
		return nil
	}
	now   := time.Now().In(loc)
	event := t.Time.In(loc)
	if event.Before(now) {
		return nil
	}
	days := int(event.Sub(now).Hours() / 24)
	return &days
}

func formatDate(t sql.NullTime, loc *time.Location) string {
	if !t.Valid {
		return "Data a confirmar"
	}
	ev := t.Time.In(loc)
	return fmt.Sprintf("%s, %d %s", diasSemana[ev.Weekday()], ev.Day(), meses[ev.Month()])
}

func formatTime(t sql.NullTime, loc *time.Location) string {
	if !t.Valid {
		return ""
	}
	ev := t.Time.In(loc)
	if ev.Minute() == 0 {
		return fmt.Sprintf("%dh", ev.Hour())
	}
	return fmt.Sprintf("%dh%02d", ev.Hour(), ev.Minute())
}

func parseLocation(raw []byte) Location {
	if len(raw) == 0 {
		return Location{}
	}
	var l Location
	if err := json.Unmarshal(raw, &l); err != nil {
		return Location{}
	}
	return l
}

func formatVenueShort(l Location) string {
	switch {
	case l.VenueName != "" && l.City != "":
		return fmt.Sprintf("%s, %s", l.VenueName, l.City)
	case l.VenueName != "":
		return l.VenueName
	case l.City != "":
		return l.City
	default:
		return "Local a definir"
	}
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

	var startDateRaw *time.Time
	if startDate.Valid {
		t := startDate.Time.In(loc)
		startDateRaw = &t
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
		DaysUntil:         calcDaysUntil(startDate, loc),
		StartDateRaw:      startDateRaw,
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