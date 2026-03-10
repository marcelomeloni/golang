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

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

type EventCard struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Slug     string `json:"slug"`
	Venue    string `json:"venue"`
	City     string `json:"city"`
	State    string `json:"state"`
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

// Location representa o JSONB da coluna `location` na tabela events.
type Location struct {
	VenueName    string `json:"venue_name"`
	Street       string `json:"street"`
	Number       string `json:"number"`
	Neighborhood string `json:"neighborhood"`
	City         string `json:"city"`
	State        string `json:"state"`
	CEP          string `json:"cep"`
	Complement   string `json:"complement"`
}

// activeFilter é a condição SQL reutilizada em todas as queries:
// evento publicado cujo end_date ainda não passou.
// Fallback: se end_date for NULL, usa start_date.
const activeFilter = `
  status = 'published'
  AND (
    (end_date IS NOT NULL AND end_date > NOW())
    OR (end_date IS NULL AND start_date > NOW())
  )`

func GetHomeEvents(c *gin.Context) {
	db := config.GetDB()
	var seenIDs []string

	// ── 1. EM DESTAQUE ────────────────────────────────────────────────────
	destaqueEvents, err := fetchEvents(db, fmt.Sprintf(`
		SELECT id, title, slug, image_url, start_date, category, location
		FROM events
		WHERE %s
		ORDER BY views DESC
		LIMIT 5`, activeFilter))
	if err != nil {
		log.Printf("GetHomeEvents destaque: %v", err)
		destaqueEvents = []EventCard{}
	}
	seenIDs = append(seenIDs, extractIDs(destaqueEvents)...)

	// ── 2. PERTO DE VOCÊ ─────────────────────────────────────────────────
	var pertoEvents []EventCard

	latStr := c.Query("lat")
	lngStr := c.Query("lng")
	hasCoords := latStr != "" && lngStr != ""

	if hasCoords {
		lat, errLat := strconv.ParseFloat(latStr, 64)
		lng, errLng := strconv.ParseFloat(lngStr, 64)

		if errLat != nil || errLng != nil {
			hasCoords = false
		} else {
			maxRadiusKm := 50.0
			if r := c.Query("radius"); r != "" {
				if parsed, err := strconv.ParseFloat(r, 64); err == nil && parsed > 0 {
					maxRadiusKm = parsed
				}
			}

			placeholder, args := buildExcludePlaceholders(seenIDs, 4)
			query := fmt.Sprintf(`
				SELECT id, title, slug, image_url, start_date, category, location
				FROM events
				WHERE %s
				  AND geolocation IS NOT NULL
				  %s
				  AND ST_DWithin(
				        geolocation,
				        ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
				        $3
				      )
				ORDER BY geolocation <-> ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
				LIMIT 5`, activeFilter, placeholder)

			queryArgs := append([]interface{}{lng, lat, maxRadiusKm * 1000}, args...)
			pertoEvents, err = fetchEvents(db, query, queryArgs...)
			if err != nil {
				log.Printf("GetHomeEvents perto (coords): %v", err)
				pertoEvents = []EventCard{}
			}
		}
	}

	if !hasCoords {
		placeholder, args := buildExcludePlaceholders(seenIDs, 1)
		query := fmt.Sprintf(`
			SELECT id, title, slug, image_url, start_date, category, location
			FROM events
			WHERE %s
			  %s
			ORDER BY start_date ASC
			LIMIT 5`, activeFilter, placeholder)

		pertoEvents, err = fetchEvents(db, query, args...)
		if err != nil {
			log.Printf("GetHomeEvents perto (sem coords): %v", err)
			pertoEvents = []EventCard{}
		}
	}

	seenIDs = append(seenIDs, extractIDs(pertoEvents)...)

	// ── 3. PRÓXIMA SEMANA ─────────────────────────────────────────────────
	placeholder, args := buildExcludePlaceholders(seenIDs, 1)
	query := fmt.Sprintf(`
		SELECT id, title, slug, image_url, start_date, category, location
		FROM events
		WHERE %s
		  AND start_date BETWEEN NOW() AND NOW() + INTERVAL '14 days'
		  %s
		ORDER BY start_date ASC
		LIMIT 5`, activeFilter, placeholder)

	proximaEvents, err := fetchEvents(db, query, args...)
	if err != nil {
		log.Printf("GetHomeEvents proxima: %v", err)
		proximaEvents = []EventCard{}
	}

	// ── RESPOSTA ──────────────────────────────────────────────────────────
	sections := []SectionResponse{
		{ID: "destaque", Label: "em destaque", Events: destaqueEvents},
		{
			ID: "perto",
			Label: func() string {
				if hasCoords {
					return "perto de você"
				}
				return "em breve"
			}(),
			Events: pertoEvents,
		},
		{ID: "proxima", Label: "próxima semana", Events: proximaEvents},
	}

	c.JSON(http.StatusOK, sections)
}

// ── HELPERS ───────────────────────────────────────────────────────────────────

func extractIDs(events []EventCard) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

func buildExcludePlaceholders(ids []string, startIdx int) (string, []interface{}) {
	if len(ids) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", startIdx+i)
		args[i] = id
	}
	return fmt.Sprintf("AND id NOT IN (%s)", strings.Join(placeholders, ",")), args
}

func fetchEvents(db *sql.DB, query string, args ...interface{}) ([]EventCard, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	loc, _ := time.LoadLocation("America/Sao_Paulo")
	diasSemana := []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
	meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}

	var events []EventCard
	for rows.Next() {
		var id, title, slug string
		var img, category sql.NullString
		var startDate sql.NullTime
		var locationJSON []byte

		if err := rows.Scan(&id, &title, &slug, &img, &startDate, &category, &locationJSON); err != nil {
			log.Printf("fetchEvents scan: %v", err)
			continue
		}

		venueStr := "Local a definir"
		var cityStr, stateStr string

		if len(locationJSON) > 0 {
			var locData Location
			if err := json.Unmarshal(locationJSON, &locData); err == nil {
				cityStr = locData.City
				stateStr = locData.State
				switch {
				case locData.VenueName != "" && locData.City != "":
					venueStr = fmt.Sprintf("%s, %s", locData.VenueName, locData.City)
				case locData.VenueName != "":
					venueStr = locData.VenueName
				case locData.City != "":
					venueStr = locData.City
				}
			}
		}

		dateStr, timeStr := "", ""
		if startDate.Valid {
			t := startDate.Time.In(loc)
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
			City:     cityStr,
			State:    stateStr,
			Date:     dateStr,
			Time:     timeStr,
			Category: category.String,
			Image:    img.String,
		})
	}

	if events == nil {
		events = []EventCard{}
	}
	return events, rows.Err()
}
