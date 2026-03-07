// controllers/client/organizador_controller.go
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
// ESTRUTURAS DE RESPOSTA
// ==========================================

type OrganizerDetailResponse struct {
	Organizer OrganizerProfile `json:"organizer"`
	Events    []OrgEvent       `json:"events"`
}

type OrganizerProfile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	LogoURL     string    `json:"logoUrl"`
	BannerURL   string    `json:"bannerUrl"`
	City        string    `json:"city"`
	Instagram   string    `json:"instagram"`
	Facebook    string    `json:"facebook"`
	Website     string    `json:"website"`
	Phone       string    `json:"phone"`
	WhatsApp    string    `json:"whatsapp"`
	Email       string    `json:"email"`
	Followers   int       `json:"followers"`
	IsFollowing bool      `json:"isFollowing"` // <-- NOVO CAMPO ADICIONADO
	Links       []OrgLink `json:"links"`
}

type OrgLink struct {
	Tipo  string `json:"tipo"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type OrgEvent struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Nome      string `json:"nome"`
	Data      string `json:"data"`
	Hora      string `json:"hora"`
	Local     string `json:"local"`
	ImageURL  string `json:"imagemUrl"`
	Status    string `json:"status"`    // "normal" | "encerrado"
	StartDate string `json:"startDate"` // ISO 8601
}

// ==========================================
// HANDLERS
// ==========================================

func GetOrganizerDetail(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug da organização é obrigatório"})
		return
	}

	db := config.GetDB()
	
	// Pega o ID do usuário (caso a rota use OptionalAuth)
	userID := c.GetString("userID") 

	// ------------------------------------------------------------------
	// 1. DADOS DA ORGANIZAÇÃO + CONTAGEM DE SEGUIDORES
	// ------------------------------------------------------------------
	queryOrg := `
		SELECT
			o.id,
			o.name,
			o.slug,
			COALESCE(o.logo_url,   ''),
			COALESCE(o.banner_url, ''),
			COALESCE(o.city,       ''),
			COALESCE(o.instagram,  ''),
			COALESCE(o.facebook,   ''),
			COALESCE(o.website,    ''),
			COALESCE(o.phone,      ''),
			COALESCE(o.whatsapp,   ''),
			COALESCE(o.email,      ''),
			COALESCE(o.links,      '[]'::jsonb),
			COUNT(f.id) AS followers
		FROM organizations o
		LEFT JOIN organization_followers f ON f.organization_id = o.id
		WHERE o.slug = $1
		GROUP BY o.id;
	`

	var (
		orgID, orgName, orgSlug      string
		logoURL, bannerURL, city     string
		instagram, facebook, website string
		phone, whatsapp, email       string
		linksJSON                    []byte
		followers                    int
	)

	err := db.QueryRow(queryOrg, slug).Scan(
		&orgID, &orgName, &orgSlug,
		&logoURL, &bannerURL, &city,
		&instagram, &facebook, &website,
		&phone, &whatsapp, &email,
		&linksJSON, &followers,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organização não encontrada"})
		return
	} else if err != nil {
		log.Printf("Erro ao buscar organização: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno do servidor"})
		return
	}

	// Verifica se o usuário logado segue esta organização
	isFollowing := false
	if userID != "" {
		err = db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM organization_followers 
				WHERE organization_id = $1 AND user_id = $2
			)
		`, orgID, userID).Scan(&isFollowing)
		if err != nil && err != sql.ErrNoRows {
			log.Printf("Erro ao verificar follow: %v", err)
		}
	}

	var links []OrgLink
	if len(linksJSON) > 0 {
		_ = json.Unmarshal(linksJSON, &links)
	}
	if links == nil {
		links = []OrgLink{}
	}

	organizer := OrganizerProfile{
		ID:          orgID,
		Name:        orgName,
		Slug:        orgSlug,
		LogoURL:     logoURL,
		BannerURL:   bannerURL,
		City:        city,
		Instagram:   instagram,
		Facebook:    facebook,
		Website:     website,
		Phone:       phone,
		WhatsApp:    whatsapp,
		Email:       email,
		Followers:   followers,
		IsFollowing: isFollowing, // Retorna true ou false pro Frontend
		Links:       links,
	}

	// ------------------------------------------------------------------
	// 2. EVENTOS DA ORGANIZAÇÃO
	// ------------------------------------------------------------------
	queryEvents := `
		SELECT
			e.id,
			e.slug,
			e.title,
			e.start_date,
			e.end_date,
			COALESCE(e.image_url, ''),
			e.status,
			COALESCE(e.location->>'venue_name', '')
		FROM events e
		WHERE e.organization_id = $1
		  AND e.status IN ('published', 'cancelled')
		ORDER BY e.start_date DESC;
	`

	rows, err := db.Query(queryEvents, orgID)
	var events []OrgEvent
	if err == nil {
		defer rows.Close()
		now := time.Now()

		for rows.Next() {
			var (
				evID, evSlug, evTitle        string
				evStartDate, evEndDate        sql.NullTime
				evImageURL, evStatus          string
				evVenue                       string
			)
			if err := rows.Scan(&evID, &evSlug, &evTitle, &evStartDate, &evEndDate, &evImageURL, &evStatus, &evVenue); err != nil {
				continue
			}

			data := "Data a definir"
			hora := ""
			startDateISO := ""
			if evStartDate.Valid {
				loc, _ := time.LoadLocation("America/Sao_Paulo")
				t := evStartDate.Time.In(loc)
				meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
				data = formatEventDate(t, meses)
				hora = formatEventTime(t)
				startDateISO = evStartDate.Time.UTC().Format(time.RFC3339)
			}

			frontendStatus := "normal"
			if evStatus == "cancelled" {
				frontendStatus = "encerrado"
			} else if evEndDate.Valid && evEndDate.Time.Before(now) {
				frontendStatus = "encerrado"
			} else if !evEndDate.Valid && evStartDate.Valid && evStartDate.Time.Before(now) {
				frontendStatus = "encerrado"
			}

			events = append(events, OrgEvent{
				ID:        evID,
				Slug:      evSlug,
				Nome:      evTitle,
				Data:      data,
				Hora:      hora,
				Local:     evVenue,
				ImageURL:  evImageURL,
				Status:    frontendStatus,
				StartDate: startDateISO,
			})
		}
	}

	if events == nil {
		events = []OrgEvent{}
	}

	c.JSON(http.StatusOK, OrganizerDetailResponse{
		Organizer: organizer,
		Events:    events,
	})
}

// ------------------------------------------------------------------
// FOLLOW / UNFOLLOW
// ------------------------------------------------------------------

func FollowOrganizer(c *gin.Context) {
	orgSlug := c.Param("slug")
	
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não autenticado"})
		return
	}

	db := config.GetDB()

	var orgID string
	if err := db.QueryRow(`SELECT id FROM organizations WHERE slug = $1`, orgSlug).Scan(&orgID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organização não encontrada"})
		return
	}

	_, err := db.Exec(`
		INSERT INTO organization_followers (organization_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, user_id) DO NOTHING;
	`, orgID, userID)
	if err != nil {
		log.Printf("Erro ao seguir organização: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao seguir organização"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"following": true})
}

func UnfollowOrganizer(c *gin.Context) {
	orgSlug := c.Param("slug")
	
	userID := c.GetString("userID")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Usuário não autenticado"})
		return
	}

	db := config.GetDB()

	var orgID string
	if err := db.QueryRow(`SELECT id FROM organizations WHERE slug = $1`, orgSlug).Scan(&orgID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organização não encontrada"})
		return
	}

	_, err := db.Exec(`
		DELETE FROM organization_followers
		WHERE organization_id = $1 AND user_id = $2;
	`, orgID, userID)
	if err != nil {
		log.Printf("Erro ao deixar de seguir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao deixar de seguir"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"following": false})
}

// ------------------------------------------------------------------
// HELPERS DE FORMATAÇÃO DE DATA
// ------------------------------------------------------------------

func formatEventDate(t time.Time, meses []string) string {
	return fmt.Sprintf("%d %s", t.Day(), meses[t.Month()])
}

func formatEventTime(t time.Time) string {
	if t.Minute() == 0 {
		return fmt.Sprintf("%dh", t.Hour())
	}
	return fmt.Sprintf("%dh%02d", t.Hour(), t.Minute())
}