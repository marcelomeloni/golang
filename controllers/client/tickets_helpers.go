package client

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Location struct {
	VenueName    string `json:"venueName"`
	Street       string `json:"street"`
	Number       string `json:"number"`
	Neighborhood string `json:"neighborhood"`
	City         string `json:"city"`
	State        string `json:"state"`
	CEP          string `json:"cep"`
}

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