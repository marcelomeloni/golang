package organizer

import (
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/internal/dbutil"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

// GetOrgEventsHandler — GET /org/:slug/events
// Lista todos os eventos da org. Acessível a qualquer membro da org.
// Query param opcional: ?status=published|draft|cancelled|finished
func GetOrgEventsHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db  := config.GetDB()
	ctx := c.Request.Context()

	// Qualquer membro pode listar — permissões granulares ficam nas subrotas
	orgID, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	query := `
		SELECT e.id, e.title, e.slug, e.status, e.image_url,
		       to_char(e.start_date, 'DD Mon, HH24:MI') AS start_date,
		       e.location->>'venue_name' AS venue,
		       e.location->>'city'       AS city,
		       COUNT(t.id)               AS tickets_sold,
		       COALESCE(
		         (SELECT SUM(tb2.quantity_total)
		            FROM ticket_batches tb2
		            JOIN ticket_categories tc2 ON tc2.id = tb2.category_id
		           WHERE tc2.event_id = e.id), 0
		       ) AS total_capacity
		  FROM events e
		  LEFT JOIN orders o  ON o.event_id = e.id AND o.status = 'paid'
		  LEFT JOIN tickets t ON t.order_id = o.id
		 WHERE e.organization_id = $1`

	args := []interface{}{orgID}

	if status := c.Query("status"); status != "" {
		query += ` AND e.status = $2`
		args = append(args, status)
	}

	query += ` GROUP BY e.id ORDER BY e.start_date DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar eventos"})
		return
	}
	defer rows.Close()

	events := []gin.H{}
	for rows.Next() {
		var (
			id, title, slug, status string
			imageURL, startDate, venue, city *string
			ticketsSold, totalCapacity       int
		)
		if err := rows.Scan(
			&id, &title, &slug, &status, &imageURL,
			&startDate, &venue, &city,
			&ticketsSold, &totalCapacity,
		); err != nil {
			continue
		}
		events = append(events, gin.H{
			"id":             id,
			"title":          title,
			"slug":           slug,
			"status":         status,
			"image_url":      imageURL,
			"start_date":     startDate,
			"venue":          venue,
			"city":           city,
			"tickets_sold":   ticketsSold,
			"total_capacity": totalCapacity,
		})
	}

	c.JSON(http.StatusOK, events)
}

// GetOrgEventDetailHandler — GET /org/:slug/events/:eventID
// Retorna dados completos do evento com categorias e lotes aninhados.
func GetOrgEventDetailHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("eventID")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db  := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// ── Dados principais ──────────────────────────────────────────────────────
	var (
		id, title, slug, status, createdAt string
		description, category, instagram   *string
		imageURL, startDate, endDate       *string
		location, requirements             *string
		views                              int
	)

	err = db.QueryRowContext(ctx,
		`SELECT id, title, slug, description, category, instagram, status,
		        image_url,
		        to_char(start_date, 'YYYY-MM-DD"T"HH24:MI:SS'),
		        to_char(end_date,   'YYYY-MM-DD"T"HH24:MI:SS'),
		        location::text, requirements::text, views,
		        to_char(created_at, 'YYYY-MM-DD')
		   FROM events
		  WHERE id = $1 AND organization_id = $2`, eventID, orgID,
	).Scan(
		&id, &title, &slug, &description, &category, &instagram, &status,
		&imageURL, &startDate, &endDate,
		&location, &requirements, &views, &createdAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// ── Categorias com lotes aninhados ────────────────────────────────────────
	rows, err := db.QueryContext(ctx,
		`SELECT
		   tc.id, tc.name, tc.type, tc.description, tc.availability,
		   tc.is_transferable, tc.in_reppy_market, tc.position,
		   tb.id,
		   tb.name,
		   tb.price,
		   tb.quantity_total,
		   tb.quantity_sold,
		   tb.status,
		   tb.fee_payer,
		   tb.sales_trigger,
		   tb.min_purchase,
		   tb.max_purchase,
		   to_char(tb.start_date, 'YYYY-MM-DD"T"HH24:MI:SS'),
		   to_char(tb.end_date,   'YYYY-MM-DD"T"HH24:MI:SS')
		  FROM ticket_categories tc
		  LEFT JOIN ticket_batches tb ON tb.category_id = tc.id
		 WHERE tc.event_id = $1
		 ORDER BY tc.position ASC, tb.created_at ASC`, eventID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar categorias"})
		return
	}
	defer rows.Close()

	type lotAgg struct {
		ID            string
		Name          string
		Price         float64
		QuantityTotal int
		QuantitySold  int
		Status        string
		FeePayer      string
		SalesTrigger  string
		MinPurchase   int
		MaxPurchase   int
		StartDate     *string
		EndDate       *string
	}
	type catAgg struct {
		ID             string
		Name           string
		Type           string
		Description    *string
		Availability   string
		IsTransferable bool
		InReppyMarket  bool
		Lots           []lotAgg
	}

	order := []string{}
	cats  := map[string]*catAgg{}

	for rows.Next() {
		var (
			catID, catName, catType, catAvail     string
			catDesc                               *string
			isTransferable, inMarket              bool
			position                              int
			batchID, batchName                    *string
			batchStatus, feePayer, salesTrigger   *string
			price                                 *float64
			qtyTotal, qtySold, minP, maxP         *int
			bStart, bEnd                          *string
		)
		if err := rows.Scan(
			&catID, &catName, &catType, &catDesc, &catAvail,
			&isTransferable, &inMarket, &position,
			&batchID, &batchName, &price,
			&qtyTotal, &qtySold, &batchStatus,
			&feePayer, &salesTrigger,
			&minP, &maxP, &bStart, &bEnd,
		); err != nil {
			continue
		}

		if _, seen := cats[catID]; !seen {
			cats[catID] = &catAgg{
				ID: catID, Name: catName, Type: catType,
				Description: catDesc, Availability: catAvail,
				IsTransferable: isTransferable, InReppyMarket: inMarket,
			}
			order = append(order, catID)
		}

		if batchID != nil {
			cats[catID].Lots = append(cats[catID].Lots, lotAgg{
				ID:            *batchID,
				Name:          dbutil.StrVal(batchName),
				Price:         dbutil.FloatVal(price),
				QuantityTotal: dbutil.IntVal(qtyTotal),
				QuantitySold:  dbutil.IntVal(qtySold),
				Status:        dbutil.StrVal(batchStatus),
				FeePayer:      dbutil.StrVal(feePayer),
				SalesTrigger:  dbutil.StrVal(salesTrigger),
				MinPurchase:   dbutil.IntVal(minP),
				MaxPurchase:   dbutil.IntVal(maxP),
				StartDate:     bStart,
				EndDate:       bEnd,
			})
		}
	}

	categoriesOut := make([]gin.H, 0, len(order))
	for _, catID := range order {
		cat := cats[catID]
		lots := make([]gin.H, 0, len(cat.Lots))
		for _, l := range cat.Lots {
			lots = append(lots, gin.H{
				"id":             l.ID,
				"name":           l.Name,
				"price":          l.Price,
				"quantity_total": l.QuantityTotal,
				"quantity_sold":  l.QuantitySold,
				"status":         l.Status,
				"fee_payer":      l.FeePayer,
				"sales_trigger":  l.SalesTrigger,
				"min_purchase":   l.MinPurchase,
				"max_purchase":   l.MaxPurchase,
				"start_date":     l.StartDate,
				"end_date":       l.EndDate,
			})
		}
		categoriesOut = append(categoriesOut, gin.H{
			"id":              cat.ID,
			"name":            cat.Name,
			"type":            cat.Type,
			"description":     cat.Description,
			"availability":    cat.Availability,
			"is_transferable": cat.IsTransferable,
			"in_reppy_market": cat.InReppyMarket,
			"lots":            lots,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                id,
		"title":             title,
		"slug":              slug,
		"description":       description,
		"category":          category,
		"instagram":         instagram,
		"status":            status,
		"image_url":         imageURL,
		"start_date":        startDate,
		"end_date":          endDate,
		"location":          location,
		"requirements":      requirements,
		"views":             views,
		"created_at":        createdAt,
		"ticket_categories": categoriesOut,
	})
}