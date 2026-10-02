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
	"bilheteria-api/services/feehelper"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS DE RESPOSTA (JSON)
// ==========================================

type EventDetailResponse struct {
	Event      EventData          `json:"event"`
	Categories []OfficialCategory `json:"categories"`
	MarketLots []MarketLot        `json:"marketLots"`
}

type EventData struct {
	ID           string        `json:"id"`
	Slug         string        `json:"slug"`
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

// OfficialCategory agrupa os lotes de uma mesma categoria
type OfficialCategory struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Description string        `json:"description"`
	Lots        []OfficialLot `json:"lots"`
}

type OfficialLot struct {
	ID                string  `json:"id"`
	Title             string  `json:"title"`
	Subtitle          string  `json:"subtitle"`
	Price             float64 `json:"price"`
	FeePayer          string  `json:"feePayer"`
	FeePercentage     float64 `json:"feePercentage"`
	Available         bool    `json:"available"`
	UnavailableReason string  `json:"unavailableReason,omitempty"`
}

type MarketLot struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Subtitle     string  `json:"subtitle"`
	CategoryName string  `json:"categoryName"`
	Price        float64 `json:"price"`
	SellerID     string  `json:"sellerId"`
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
	MinAge         string   `json:"min_age"`
	RequiredDocs   []string `json:"required_docs"`
	AcceptedTerms  bool     `json:"accepted_terms"`
	AgeRestriction string   `json:"age_restriction"`
	Documents      string   `json:"documents"`
	RefundPolicy   string   `json:"refund_policy"`
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
			RETURNING id, title, description, image_url, start_date, location, requirements,
			          organization_id, instagram, promo_fee
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
			u.promo_fee,
			COALESCE(o.name, ''),
			COALESCE(o.slug, ''),
			COALESCE(o.logo_url, '')
		FROM updated u
		LEFT JOIN organizations o ON u.organization_id = o.id;
	`

	var (
		id, title                      string
		description, imageURL          sql.NullString
		startDate                      sql.NullTime
		locationJSON, requirementsJSON []byte
		instagram                      sql.NullString
		promoFee                       bool
		orgName, orgSlug, orgLogoURL   string
	)

	err := db.QueryRow(queryEvent, slug).Scan(
		&id, &title, &description, &imageURL, &startDate,
		&locationJSON, &requirementsJSON,
		&instagram,
		&promoFee,
		&orgName, &orgSlug, &orgLogoURL,
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
		Slug:         slug,
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
		// PlatformFee no nível do evento fica zerado — a taxa real está em cada OfficialLot.feePercentage
		PlatformFee: PlatformFee{Percentage: 0, Fixed: 0},
		Organizer: OrganizerData{
			Name:    orgName,
			Slug:    orgSlug,
			LogoURL: orgLogoURL,
		},
	}

	// ------------------------------------------------------------------
	// 2. LOTES OFICIAIS — agrupados por categoria, ordenados por posição
	// ------------------------------------------------------------------
	queryOfficial := `
		SELECT
			tb.id,
			tb.name,
			COALESCE(tb.description, ''),
			tb.price,
			COALESCE(tb.fee_payer, 'buyer'),
			(
				tb.status = 'active'
				AND (tb.start_date IS NULL OR tb.start_date <= NOW())
				AND (tb.end_date   IS NULL OR tb.end_date   >  NOW())
				AND tb.quantity_sold < tb.quantity_total
			) AS available,
			CASE
				WHEN tb.quantity_sold >= tb.quantity_total                  THEN 'sold_out'
				WHEN tb.end_date   IS NOT NULL AND tb.end_date   <= NOW()   THEN 'expired'
				WHEN tb.start_date IS NOT NULL AND tb.start_date >  NOW()   THEN 'not_started'
				ELSE ''
			END AS unavailable_reason,
			COALESCE(tc.id::text, ''),
			COALESCE(tc.name, 'Geral'),
			COALESCE(tc.type, ''),
			COALESCE(tc.description, ''),
			COALESCE(tc.position, 9999)
		FROM ticket_batches tb
		LEFT JOIN ticket_categories tc ON tc.id = tb.category_id
		WHERE tb.event_id = $1
		ORDER BY COALESCE(tc.position, 9999) ASC, tb.price ASC;
	`

	rowsOfficial, err := db.Query(queryOfficial, id)

	// slice para preservar a ordem de aparição das categorias
	categoryOrder := []string{}
	categoryMap := map[string]*OfficialCategory{}

	if err == nil {
		defer rowsOfficial.Close()
		for rowsOfficial.Next() {
			var l OfficialLot
			var catID, catName, catType, catDesc string
			var catPosition int

			if err := rowsOfficial.Scan(
				&l.ID, &l.Title, &l.Subtitle, &l.Price, &l.FeePayer,
				&l.Available, &l.UnavailableReason,
				&catID, &catName, &catType, &catDesc, &catPosition,
			); err != nil {
				log.Printf("GetEventDetail scan lot: %v", err)
				continue
			}

			// Calcula a taxa correta para este lote específico
			if l.FeePayer == "buyer" && l.Price > 0 {
				l.FeePercentage = feehelper.CalcFee(l.Price, promoFee).FeePercentage
			}

			// Cria a categoria na primeira aparição, preservando a ordem
			if _, exists := categoryMap[catID]; !exists {
				categoryMap[catID] = &OfficialCategory{
					ID:          catID,
					Name:        catName,
					Type:        catType,
					Description: catDesc,
					Lots:        []OfficialLot{},
				}
				categoryOrder = append(categoryOrder, catID)
			}
			categoryMap[catID].Lots = append(categoryMap[catID].Lots, l)
		}
	} else {
		log.Printf("GetEventDetail officialLots: %v", err)
	}

	// Converte mapa → slice respeitando a ordem original
	categories := make([]OfficialCategory, 0, len(categoryOrder))
	for _, cid := range categoryOrder {
		categories = append(categories, *categoryMap[cid])
	}

	// ------------------------------------------------------------------
	// 3. REPPY MARKET — somente categorias com in_reppy_market = true
	// ------------------------------------------------------------------
	queryMarket := `
		SELECT
			m.id,
			tb.name,
			COALESCE(tb.description, ''),
			tc.name AS category_name,
			m.price,
			m.seller_id
		FROM market_listings m
		JOIN tickets           t  ON m.ticket_id = t.id
		JOIN ticket_batches    tb ON t.batch_id   = tb.id
		JOIN ticket_categories tc ON tc.id        = tb.category_id
		WHERE m.event_id = $1
		  AND m.status   = 'active'
		  AND t.status   = 'valid'
		  AND tc.in_reppy_market = true
		ORDER BY m.price ASC;
	`

	rowsMarket, err := db.Query(queryMarket, id)
	var marketLots []MarketLot
	if err == nil {
		defer rowsMarket.Close()
		for rowsMarket.Next() {
			var m MarketLot
			if err := rowsMarket.Scan(
				&m.ID, &m.Title, &m.Subtitle, &m.CategoryName, &m.Price, &m.SellerID,
			); err != nil {
				log.Printf("GetEventDetail scan market: %v", err)
				continue
			}
			marketLots = append(marketLots, m)
		}
	} else {
		log.Printf("GetEventDetail marketLots: %v", err)
	}

	if categories == nil {
		categories = []OfficialCategory{}
	}
	if marketLots == nil {
		marketLots = []MarketLot{}
	}

	c.JSON(http.StatusOK, EventDetailResponse{
		Event:      eventData,
		Categories: categories,
		MarketLots: marketLots,
	})
}