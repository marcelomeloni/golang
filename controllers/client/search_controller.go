package client

import (
	"log"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// SearchResult é o item unificado retornado pela busca.
type SearchResult struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "event" | "organization"
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
	Slug     string `json:"slug"`
}

// Search → GET /client/search?q=...
//
// Busca simultânea em eventos (title) e organizações (name).
// Retorna no máximo 5 eventos + 3 organizações para alimentar
// um dropdown de autocomplete em tempo real.
func Search(c *gin.Context) {
	q := c.Query("q")
	if len(q) < 2 {
		c.JSON(http.StatusOK, gin.H{"results": []SearchResult{}})
		return
	}

	db := config.GetDB()
	pattern := "%" + q + "%"
	results := make([]SearchResult, 0, 8)

	// ── Eventos ───────────────────────────────────────────────────────────
eventRows, err := db.Query(`
    SELECT id, title,
           COALESCE(location->>'venue_name', '') || ', ' || COALESCE(location->>'city', '') AS subtitle,
           image_url, slug
    FROM events
    WHERE status = 'published'
  AND (end_date IS NULL OR end_date > NOW())
  AND (
      title                        ILIKE $1
      OR location->>'city'         ILIKE $1
      OR location->>'venue_name'   ILIKE $1
      OR location->>'neighborhood' ILIKE $1
  )

    ORDER BY start_date ASC
    LIMIT 5`,
    pattern,
)
	if err != nil {
		log.Printf("Search events: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na busca"})
		return
	}
	defer eventRows.Close()

	for eventRows.Next() {
    var r SearchResult
    var imageURL *string
    if err := eventRows.Scan(&r.ID, &r.Title, &r.Subtitle, &imageURL, &r.Slug); err != nil {
        log.Printf("Search events scan: %v", err)
        continue
    }
    r.Type = "event"
    if imageURL != nil {
        r.ImageURL = *imageURL
    }
    results = append(results, r)
}

	// ── Organizações ──────────────────────────────────────────────────────
	orgRows, err := db.Query(`
		SELECT id, name, city, logo_url, slug
		FROM organizations
		WHERE name ILIKE $1
		ORDER BY name ASC
		LIMIT 3`,
		pattern,
	)
	if err != nil {
		log.Printf("Search organizations: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na busca"})
		return
	}
	defer orgRows.Close()

	for orgRows.Next() {
		var r SearchResult
		var city, logoURL *string
		if err := orgRows.Scan(&r.ID, &r.Title, &city, &logoURL, &r.Slug); err != nil {
			log.Printf("Search organizations scan: %v", err)
			continue
		}
		r.Type = "organization"
		if city != nil {
			r.Subtitle = *city
		}
		if logoURL != nil {
			r.ImageURL = *logoURL
		}
		results = append(results, r)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}