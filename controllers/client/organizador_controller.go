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
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Slug       string      `json:"slug"`
	LogoURL    string      `json:"logoUrl"`
	BannerURL  string      `json:"bannerUrl"`
	City       string      `json:"city"`
	Instagram  string      `json:"instagram"`
	Facebook   string      `json:"facebook"`
	Website    string      `json:"website"`
	Phone      string      `json:"phone"`
	WhatsApp   string      `json:"whatsapp"`
	Email      string      `json:"email"`
	Followers  int         `json:"followers"`
	Links      []OrgLink   `json:"links"`
}

type OrgLink struct {
	Tipo  string `json:"tipo"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type OrgEvent struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Nome     string `json:"nome"`
	Data     string `json:"data"`
	Hora     string `json:"hora"`
	Local    string `json:"local"`
	ImageURL string `json:"imagemUrl"`
	Status   string `json:"status"` // "normal" | "encerrado" | "cancelado"
}

// ==========================================
// HANDLER
// ==========================================

func GetOrganizerDetail(c *gin.Context) {
	slug := c.Param("slug")
	if slug == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Slug da organização é obrigatório"})
		return
	}

	db := config.GetDB()

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
		orgID, orgName, orgSlug               string
		logoURL, bannerURL, city              string
		instagram, facebook, website          string
		phone, whatsapp, email               string
		linksJSON                             []byte
		followers                             int
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

	// Parse links JSONB
	var links []OrgLink
	if len(linksJSON) > 0 {
		_ = json.Unmarshal(linksJSON, &links)
	}
	if links == nil {
		links = []OrgLink{}
	}

	organizer := OrganizerProfile{
		ID:        orgID,
		Name:      orgName,
		Slug:      orgSlug,
		LogoURL:   logoURL,
		BannerURL: bannerURL,
		City:      city,
		Instagram: instagram,
		Facebook:  facebook,
		Website:   website,
		Phone:     phone,
		WhatsApp:  whatsapp,
		Email:     email,
		Followers: followers,
		Links:     links,
	}

	// ------------------------------------------------------------------
	// 2. EVENTOS DA ORGANIZAÇÃO
	// Traz publicados e cancelados/finalizados para separar no frontend
	// ------------------------------------------------------------------
	queryEvents := `
		SELECT
			e.id,
			e.slug,
			e.title,
			e.start_date,
			COALESCE(e.image_url, ''),
			e.status,
			COALESCE(e.location->>'venue_name', '')
		FROM events e
		WHERE e.organization_id = $1
		  AND e.status IN ('published', 'finished', 'cancelled')
		ORDER BY e.start_date DESC;
	`

	rows, err := db.Query(queryEvents, orgID)
	var events []OrgEvent
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var (
				evID, evSlug, evTitle string
				evStartDate           sql.NullTime
				evImageURL, evStatus  string
				evVenue               string
			)
			if err := rows.Scan(&evID, &evSlug, &evTitle, &evStartDate, &evImageURL, &evStatus, &evVenue); err != nil {
				continue
			}

			data := "Data a definir"
			hora := ""
			if evStartDate.Valid {
				loc, _ := time.LoadLocation("America/Sao_Paulo")
				t := evStartDate.Time.In(loc)
				meses := []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
				data = formatEventDate(t, meses)
				hora = formatEventTime(t)
			}

			// Mapeia status do banco para o que o frontend espera
			frontendStatus := "normal"
			if evStatus == "finished" || evStatus == "cancelled" {
				frontendStatus = "encerrado"
			}

			events = append(events, OrgEvent{
				ID:       evID,
				Slug:     evSlug,
				Nome:     evTitle,
				Data:     data,
				Hora:     hora,
				Local:    evVenue,
				ImageURL: evImageURL,
				Status:   frontendStatus,
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

type FollowRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

func FollowOrganizer(c *gin.Context) {
	orgSlug := c.Param("slug")
	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id é obrigatório"})
		return
	}

	db := config.GetDB()

	// Resolve org ID a partir do slug
	var orgID string
	if err := db.QueryRow(`SELECT id FROM organizations WHERE slug = $1`, orgSlug).Scan(&orgID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Organização não encontrada"})
		return
	}

	_, err := db.Exec(`
		INSERT INTO organization_followers (organization_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (organization_id, user_id) DO NOTHING;
	`, orgID, req.UserID)
	if err != nil {
		log.Printf("Erro ao seguir organização: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao seguir organização"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"following": true})
}

func UnfollowOrganizer(c *gin.Context) {
	orgSlug := c.Param("slug")
	var req FollowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id é obrigatório"})
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
	`, orgID, req.UserID)
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