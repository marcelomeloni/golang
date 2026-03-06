package client

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bilheteria-api/config" // ATENÇÃO: Substitua pelo nome real do seu módulo
	"github.com/gin-gonic/gin"
)

type EventCard struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Venue    string `json:"venue"`
	Date     string `json:"date"`
	Time     string `json:"time"`
	Category string `json:"category"`
	Image    string `json:"image"`
}

type SectionResponse struct {
	ID     string      `json:"id"`
	Label  string      `json:"label"`
	Events []EventCard `json:"events"`
}

type Location struct {
	VenueName string `json:"venue_name"`
	City      string `json:"city"`
	State     string `json:"state"`
}

func GetHomeEvents(c *gin.Context) {
	db := config.GetDB()
	var seenIDs []string // Vai guardar os IDs já exibidos nas seções anteriores

	// ---------------------------------------------------
	// 1. EM DESTAQUE
	// ---------------------------------------------------
	destaqueQuery := `
		SELECT id, title, slug, image_url, start_date, category, location
		FROM events
		WHERE status = 'published' AND start_date > NOW()
		ORDER BY views DESC LIMIT 5`
	
	destaqueEvents, err := fetchEvents(db, destaqueQuery)
	if err != nil {
		log.Printf("Erro ao buscar eventos em destaque: %v", err)
	}
	// Adiciona os IDs encontrados na nossa lista de "já vistos"
	seenIDs = append(seenIDs, extractIDs(destaqueEvents)...)

	// ---------------------------------------------------
	// 2. PERTO DE VOCÊ 
	// ---------------------------------------------------
	latStr := c.Query("lat")
	lngStr := c.Query("lng")
	
	var pertoQuery string
	var pertoEvents []EventCard

	// Helper para não repetir os eventos que já saíram no Destaque
	excludeSQL := buildNotInClause(seenIDs)

	if latStr != "" && lngStr != "" {
		lat, _ := strconv.ParseFloat(latStr, 64)
		lng, _ := strconv.ParseFloat(lngStr, 64)
		
		maxRadiusKm := 50.0 
		if r := c.Query("radius"); r != "" {
			if parsedRadius, err := strconv.ParseFloat(r, 64); err == nil {
				maxRadiusKm = parsedRadius
			}
		}
		maxRadiusMeters := maxRadiusKm * 1000

		pertoQuery = `
			SELECT id, title, slug, image_url, start_date, category, location
			FROM events
			WHERE status = 'published' 
			  AND start_date > NOW()
			  AND geolocation IS NOT NULL ` + excludeSQL + `
			  AND ST_DWithin(geolocation, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
			ORDER BY geolocation <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
			LIMIT 5`
			
		pertoEvents, err = fetchEvents(db, pertoQuery, lng, lat, maxRadiusMeters)
	} else {
		pertoQuery = `
			SELECT id, title, slug, image_url, start_date, category, location
			FROM events
			WHERE status = 'published' AND start_date > NOW() ` + excludeSQL + `
			ORDER BY start_date ASC LIMIT 5`
		pertoEvents, err = fetchEvents(db, pertoQuery)
	}

	if err != nil {
		log.Printf("Erro em Perto de você: %v", err)
	}
	
	// Adiciona os IDs de "Perto de você" na lista de já vistos
	seenIDs = append(seenIDs, extractIDs(pertoEvents)...)

	// ---------------------------------------------------
	// 3. PRÓXIMA SEMANA
	// ---------------------------------------------------
	// Agora ele vai ignorar o que já saiu no Destaque E no Perto de Você
	excludeSQL = buildNotInClause(seenIDs)

	proximaQuery := `
		SELECT id, title, slug, image_url, start_date, category, location
		FROM events
		WHERE status = 'published' AND start_date BETWEEN NOW() AND NOW() + INTERVAL '14 days' 
		` + excludeSQL + `
		ORDER BY start_date ASC LIMIT 5`
	
	proximaEvents, err := fetchEvents(db, proximaQuery)
	if err != nil {
		log.Printf("Erro ao buscar eventos da próxima semana: %v", err)
	}

	// ---------------------------------------------------
	// MONTANDO A RESPOSTA FINAL
	// ---------------------------------------------------
	sections := []SectionResponse{
		{ID: "destaque", Label: "em destaque", Events: destaqueEvents},
		{ID: "perto", Label: "perto de você", Events: pertoEvents},
		{ID: "proxima", Label: "próxima semana", Events: proximaEvents},
	}

	c.JSON(http.StatusOK, sections)
}

// ---------------------------------------------------
// FUNÇÕES AUXILIARES
// ---------------------------------------------------

// extractIDs pega uma lista de eventos e retorna só os IDs em um slice de strings
func extractIDs(events []EventCard) []string {
	var ids []string
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

// buildNotInClause monta a parte "AND id NOT IN ('uuid1', 'uuid2')" da query
func buildNotInClause(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	var quotedIDs []string
	for _, id := range ids {
		quotedIDs = append(quotedIDs, fmt.Sprintf("'%s'", id))
	}
	return fmt.Sprintf(" AND id NOT IN (%s) ", strings.Join(quotedIDs, ","))
}

// fetchEvents executa a query e formata os dados
func fetchEvents(db *sql.DB, query string, args ...interface{}) ([]EventCard, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []EventCard
	loc, _ := time.LoadLocation("America/Sao_Paulo")

	for rows.Next() {
		var id, title, slug string
		var img, category sql.NullString
		var startDate sql.NullTime
		var locationJSON []byte

		err := rows.Scan(&id, &title, &slug, &img, &startDate, &category, &locationJSON)
		if err != nil {
			log.Printf("Erro no scan: %v", err)
			continue
		}

		// Parse do JSONB Location
		var locData Location
		venueStr := "Local a definir"
		if len(locationJSON) > 0 {
			if err := json.Unmarshal(locationJSON, &locData); err == nil {
				if locData.VenueName != "" && locData.City != "" {
					venueStr = fmt.Sprintf("%s, %s", locData.VenueName, locData.City)
				} else if locData.VenueName != "" {
					venueStr = locData.VenueName
				}
			}
		}

		// Formatação de Data e Hora
		dateStr, timeStr := "", ""
		if startDate.Valid {
			t := startDate.Time.In(loc)
			diasSemana := []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
			meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
			
			dateStr = fmt.Sprintf("%s, %d %s", diasSemana[t.Weekday()], t.Day(), meses[t.Month()])
			
			if t.Minute() == 0 {
				timeStr = fmt.Sprintf("%dh", t.Hour())
			} else {
				timeStr = fmt.Sprintf("%dh%02d", t.Hour(), t.Minute())
			}
		}

		events = append(events, EventCard{
			ID:       id,
			Title:    title,
			Slug:     slug,
			Venue:    venueStr,
			Date:     dateStr,
			Time:     timeStr,
			Category: category.String,
			Image:    img.String,
		})
	}

	if events == nil {
		events = []EventCard{}
	}

	return events, nil
}