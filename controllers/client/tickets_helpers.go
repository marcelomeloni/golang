package client

import (
	"database/sql"
	"log"
	"time"
)

func groupTickets(rows *sql.Rows) (proximos, passados []MyTicket, err error) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")

	proximos = []MyTicket{}
	passados  = []MyTicket{}

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