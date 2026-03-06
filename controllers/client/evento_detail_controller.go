// controllers/client/evento_detail_controller.go
package client

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS DE RESPOSTA (JSON)
// ==========================================

type EventDetailResponse struct {
	Event        EventData     `json:"event"`
	OfficialLots []OfficialLot `json:"officialLots"`
	MarketLots   []MarketLot   `json:"marketLots"`
}

type EventData struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	ImageURL     string        `json:"image_url"`
	Instagram    string        `json:"instagram"`
	Date         string        `json:"date"`
	LocationName string        `json:"locationName"`
	Address      Address       `json:"address"`
	Policies     EventPolicies `json:"policies"`
	PlatformFee  PlatformFee   `json:"platformFee"`
	Organizer    OrganizerData `json:"organizer"`
}

type OrganizerData struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	LogoURL string `json:"logoUrl"`
}

type Address struct {
	Street       string `json:"street"`
	City         string `json:"city"`
	State        string `json:"state"`
	ZipCode      string `json:"zipCode"`
	Neighborhood string `json:"neighborhood"`
}

type EventPolicies struct {
	MinAge       string   `json:"minAge"`
	RequiredDocs []string `json:"requiredDocs"`
	RefundPolicy string   `json:"refundPolicy"`
}

type PlatformFee struct {
	Percentage float64 `json:"percentage"`
	Fixed      float64 `json:"fixed"`
}

type OfficialLot struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Subtitle          string  `json:"subtitle"`
	Price             float64 `json:"price"`
	FeePayer          string  `json:"feePayer"`
	Available         bool    `json:"available"`
	UnavailableReason string  `json:"unavailableReason,omitempty"` // "sold_out" | "expired" | "not_started"
}

type MarketLot struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	Price    float64 `json:"price"`
}

// ==========================================
// ESTRUTURAS AUXILIARES (decodificação JSONB)
// ==========================================

type DBLocation struct {
	VenueName    string `json:"venue_name"`
	Street       string `json:"street"`
	City         string `json:"city"`
	State        string `json:"state"`
	Cep          string `json:"cep"`
	Neighborhood string `json:"neighborhood"`
}

type DBRequirements struct {
	MinAge        string   `json:"min_age"`
	RequiredDocs  []string `json:"required_docs"`
	AcceptedTerms bool     `json:"accepted_terms"`
	// campos legados
	AgeRestriction string `json:"age_restriction"`
	Documents      string `json:"documents"`
	RefundPolicy   string `json:"refund_policy"`
}

// ==========================================
// CONTROLLER (HANDLER)
// ==========================================

func GetEventDetail(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug do evento é obrigatório"})
		return
	}

	db := config.GetDB()

	// ------------------------------------------------------------------
	// 1. ATUALIZA VIEWS E BUSCA EVENTO + ORGANIZAÇÃO
	// ------------------------------------------------------------------
	queryEvent := `
		WITH updated AS (
			UPDATE events
			SET views = views + 1
			WHERE slug = $1 AND status = 'published'
			RETURNING id, title, description, image_url, start_date, location, requirements, organization_id, instagram
		)
		SELECT
			u.id,
			u.title,
			u.description,
			u.image_url,
			u.start_date,
			u.location,
			u.requirements,
			u.instagram,
			COALESCE(o.name, ''),
			COALESCE(o.slug, ''),
			COALESCE(o.logo_url, ''),
			COALESCE(o.platform_fee_percentage, 0),
			COALESCE(o.platform_fee_fixed, 0)
		FROM updated u
		LEFT JOIN organizations o ON u.organization_id = o.id;
	`

	var (
		id, title                      string
		description, imageURL          sql.NullString
		startDate                      sql.NullTime
		locationJSON, requirementsJSON []byte
		instagram                      sql.NullString
		orgName, orgSlug, orgLogoURL   string
		orgFeePercent, orgFeeFixed     float64
	)

	err := db.QueryRow(queryEvent, slug).Scan(
		&id, &title, &description, &imageURL, &startDate,
		&locationJSON, &requirementsJSON,
		&instagram,
		&orgName, &orgSlug, &orgLogoURL,
		&orgFeePercent, &orgFeeFixed,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Evento não encontrado ou indisponível"})
		return
	} else if err != nil {
		log.Printf("Erro ao buscar detalhes do evento: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno do servidor"})
		return
	}

	// Parse localização
	var locData DBLocation
	if len(locationJSON) > 0 {
		_ = json.Unmarshal(locationJSON, &locData)
	}

	// Parse requirements
	var reqData DBRequirements
	if len(requirementsJSON) > 0 {
		_ = json.Unmarshal(requirementsJSON, &reqData)
	}

	minAge := reqData.MinAge
	if minAge == "" {
		minAge = reqData.AgeRestriction
	}
	if minAge == "" {
		minAge = "Livre"
	}

	refundPolicy := reqData.RefundPolicy
	if refundPolicy == "" {
		refundPolicy = "Cancelamentos sujeitos à política do organizador e da plataforma."
	}

	requiredDocs := reqData.RequiredDocs
	if len(requiredDocs) == 0 && reqData.Documents != "" {
		requiredDocs = []string{reqData.Documents}
	}
	if len(requiredDocs) == 0 {
		requiredDocs = []string{}
	}

	dateStr := "Data a definir"
	if startDate.Valid {
		loc, _ := time.LoadLocation("America/Sao_Paulo")
		t := startDate.Time.In(loc)
		diasSemana := []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
		meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
		dateStr = fmt.Sprintf("%s, %d de %s às %dh%02d",
			diasSemana[t.Weekday()], t.Day(), meses[t.Month()], t.Hour(), t.Minute())
	}

	eventData := EventData{
		ID:           id,
		Title:        title,
		Description:  description.String,
		ImageURL:     imageURL.String,
		Date:         dateStr,
		Instagram:    instagram.String,
		LocationName: locData.VenueName,
		Address: Address{
			Street:       locData.Street,
			City:         locData.City,
			State:        locData.State,
			ZipCode:      locData.Cep,
			Neighborhood: locData.Neighborhood,
		},
		Policies: EventPolicies{
			MinAge:       minAge,
			RequiredDocs: requiredDocs,
			RefundPolicy: refundPolicy,
		},
		PlatformFee: PlatformFee{
			Percentage: orgFeePercent,
			Fixed:      orgFeeFixed,
		},
		Organizer: OrganizerData{
			Name:    orgName,
			Slug:    orgSlug,
			LogoURL: orgLogoURL,
		},
	}

	// ------------------------------------------------------------------
	// 2. LOTES OFICIAIS — retorna todos, com flag de disponibilidade
	// ------------------------------------------------------------------
	queryOfficial := `
		SELECT
			id,
			name,
			COALESCE(description, ''),
			price,
			COALESCE(fee_payer, 'buyer'),
			(
				status = 'active'
				AND (start_date IS NULL OR start_date <= NOW())
				AND (end_date IS NULL OR end_date > NOW())
				AND quantity_sold < quantity_total
			) AS available,
			CASE
				WHEN quantity_sold >= quantity_total               THEN 'sold_out'
				WHEN end_date IS NOT NULL AND end_date <= NOW()    THEN 'expired'
				WHEN start_date IS NOT NULL AND start_date > NOW() THEN 'not_started'
				ELSE ''
			END AS unavailable_reason
		FROM ticket_batches
		WHERE event_id = $1
		ORDER BY price ASC;
	`
	rowsOfficial, err := db.Query(queryOfficial, id)
	var officialLots []OfficialLot
	if err == nil {
		defer rowsOfficial.Close()
		for rowsOfficial.Next() {
			var l OfficialLot
			if err := rowsOfficial.Scan(
				&l.ID, &l.Title, &l.Subtitle, &l.Price, &l.FeePayer,
				&l.Available, &l.UnavailableReason,
			); err != nil {
				log.Printf("GetEventDetail scan lot: %v", err)
				continue
			}
			officialLots = append(officialLots, l)
		}
	} else {
		log.Printf("GetEventDetail officialLots: %v", err)
	}

	// ------------------------------------------------------------------
	// 3. REPPY MARKET — apenas listings ativos com ticket válido
	// ------------------------------------------------------------------
	queryMarket := `
		SELECT m.id, tb.name, COALESCE(tb.description, ''), m.price
		FROM market_listings m
		JOIN tickets t         ON m.ticket_id = t.id
		JOIN ticket_batches tb ON t.batch_id  = tb.id
		WHERE m.event_id = $1
		  AND m.status = 'active'
		  AND t.status = 'valid'
		ORDER BY m.price ASC;
	`
	rowsMarket, err := db.Query(queryMarket, id)
	var marketLots []MarketLot
	if err == nil {
		defer rowsMarket.Close()
		for rowsMarket.Next() {
			var m MarketLot
			if err := rowsMarket.Scan(&m.ID, &m.Title, &m.Subtitle, &m.Price); err != nil {
				log.Printf("GetEventDetail scan market: %v", err)
				continue
			}
			marketLots = append(marketLots, m)
		}
	} else {
		log.Printf("GetEventDetail marketLots: %v", err)
	}

	if officialLots == nil {
		officialLots = []OfficialLot{}
	}
	if marketLots == nil {
		marketLots = []MarketLot{}
	}

	c.JSON(http.StatusOK, EventDetailResponse{
		Event:        eventData,
		OfficialLots: officialLots,
		MarketLots:   marketLots,
	})
}