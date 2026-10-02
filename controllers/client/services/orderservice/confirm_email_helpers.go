package orderservice

// confirm_email_helpers.go
// Funções auxiliares de formatação usadas em confirm_email.go.
// São análogas às de client/tickets.go mas vivem no orderservice para
// evitar dependência entre packages client ↔ orderservice.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Location espelha o JSON armazenado na coluna events.location.
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

func loadLocation() *time.Location {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	return loc
}

func parseLocationJSON(raw []byte) Location {
	if len(raw) == 0 {
		return Location{}
	}
	var l Location
	_ = json.Unmarshal(raw, &l)
	return l
}

func formatEmailDate(t sql.NullTime, loc *time.Location) string {
	if !t.Valid {
		return "Data a confirmar"
	}
	ev := t.Time.In(loc)
	return fmt.Sprintf("%s, %d %s", diasSemana[ev.Weekday()], ev.Day(), meses[ev.Month()])
}

func formatEmailTime(t sql.NullTime, loc *time.Location) string {
	if !t.Valid {
		return ""
	}
	ev := t.Time.In(loc)
	if ev.Minute() == 0 {
		return fmt.Sprintf("%dh", ev.Hour())
	}
	return fmt.Sprintf("%dh%02d", ev.Hour(), ev.Minute())
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