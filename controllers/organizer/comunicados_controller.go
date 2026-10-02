package organizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/orgservice"
	"github.com/gin-gonic/gin"
)

const reppyLogoURL = "https://waxdhvdawhcpdjyxjqkh.supabase.co/storage/v1/object/public/reppy-media/verde.png"

type comunicadoRecipient struct {
	ID            string  `json:"id"`
	FullName      string  `json:"full_name"`
	Email         string  `json:"email"`
	TicketType    string  `json:"ticket_type"`
	PaymentStatus string  `json:"payment_status"`
	BatchName     string  `json:"batch_name"`
	CategoryName  *string `json:"category_name"`
}

type sendComunicadoRequest struct {
	SenderName string             `json:"sender_name" binding:"required"`
	ReplyTo    string             `json:"reply_to"    binding:"required"`
	Subject    string             `json:"subject"     binding:"required"`
	Message    string             `json:"message"     binding:"required"`
	Filters    []comunicadoFilter `json:"filters"`
}

type comunicadoFilter struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type eventEmailData struct {
	Title    string
	ImageURL string
	Date     string
	Venue    string
	City     string
	State    string
}

type dbLocation struct {
	VenueName string `json:"venue_name"`
	City      string `json:"city"`
	State     string `json:"state"`
}

func GetComunicadosRecipientsHandler(c *gin.Context) {
	slug    := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db  := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, slug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	if !eventBelongsToOrg(ctx, db, eventID, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	recipients, err := loadComunicadoRecipients(db, eventID)
	if err != nil {
		log.Printf("GetComunicadosRecipients eventID=%s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar destinatários"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": recipients})
}

func SendComunicadoHandler(c *gin.Context) {
	slug    := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db  := config.GetDB()
	ctx := c.Request.Context()

	orgID, err := orgservice.ResolveOrgWithPermission(ctx, db, slug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	if !eventBelongsToOrg(ctx, db, eventID, orgID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	var req sendComunicadoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}

	eventData, err := loadEventEmailData(db, eventID)
	if err != nil {
		log.Printf("SendComunicado loadEventEmailData eventID=%s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar dados do evento"})
		return
	}

	allRecipients, err := loadComunicadoRecipients(db, eventID)
	if err != nil {
		log.Printf("SendComunicado loadRecipients eventID=%s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar destinatários"})
		return
	}

	recipients := applyFilters(allRecipients, req.Filters)
	recipients  = deduplicateByEmail(recipients)

	if len(recipients) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nenhum destinatário encontrado com os filtros aplicados"})
		return
	}

	to := make([]emailsender.Recipient, 0, len(recipients))
	for _, r := range recipients {
		if r.Email == "" {
			continue
		}
		to = append(to, emailsender.Recipient{Name: r.FullName, Email: r.Email})
	}

	from := os.Getenv("RESEND_FROM")
	if from == "" {
		from = "Reppy <noreply@reppy.app.br>"
	}

	sender := emailsender.New("")
	_, err = sender.Send(emailsender.Message{
		From:     fmt.Sprintf("%s <%s>", req.SenderName, extractFromAddress(from)),
		To:       to,
		Subject:  req.Subject,
		HTMLBody: wrapComunicadoHTML(req.Message, req.SenderName, req.ReplyTo, eventData),
	})
	if err != nil {
		log.Printf("SendComunicado Send eventID=%s: %v", eventID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao enviar comunicado"})
		return
	}

	log.Printf("SendComunicado: eventID=%s destinatários=%d assunto=%q ✓", eventID, len(to), req.Subject)
	c.JSON(http.StatusOK, gin.H{"sent": len(to)})
}

func loadEventEmailData(db *sql.DB, eventID string) (eventEmailData, error) {
	var (
		title        string
		imageURL     sql.NullString
		startDate    sql.NullTime
		locationJSON []byte
	)

	err := db.QueryRow(`
		SELECT title, image_url, start_date, location
		FROM events
		WHERE id = $1
	`, eventID).Scan(&title, &imageURL, &startDate, &locationJSON)
	if err != nil {
		return eventEmailData{}, err
	}

	var loc dbLocation
	if len(locationJSON) > 0 {
		_ = json.Unmarshal(locationJSON, &loc)
	}

	dateStr := ""
	if startDate.Valid {
		brasilia, _ := time.LoadLocation("America/Sao_Paulo")
		t := startDate.Time.In(brasilia)
		meses := []string{"", "janeiro", "fevereiro", "março", "abril", "maio", "junho",
			"julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
		dateStr = fmt.Sprintf("%d de %s de %d às %02dh%02d",
			t.Day(), meses[t.Month()], t.Year(), t.Hour(), t.Minute())
	}

	return eventEmailData{
		Title:    title,
		ImageURL: imageURL.String,
		Date:     dateStr,
		Venue:    loc.VenueName,
		City:     loc.City,
		State:    loc.State,
	}, nil
}

func eventBelongsToOrg(ctx context.Context, db *sql.DB, eventID, orgID string) bool {
	var exists bool
	_ = db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)`,
		eventID, orgID,
	).Scan(&exists)
	return exists
}

func loadComunicadoRecipients(db *sql.DB, eventID string) ([]comunicadoRecipient, error) {
	rows, err := db.Query(`
		SELECT
			u.id,
			COALESCE(u.full_name, 'Visitante') AS full_name,
			COALESCE(u.email, '')              AS email,
			COALESCE(tb.type, 'paid')          AS ticket_type,
			o.status                           AS payment_status,
			COALESCE(tb.name, '')              AS batch_name,
			tc.name                            AS category_name
		FROM tickets t
		JOIN orders          o  ON o.id  = t.order_id
		JOIN users           u  ON u.id  = t.user_id
		JOIN ticket_batches  tb ON tb.id = t.batch_id
		LEFT JOIN ticket_categories tc ON tc.id = tb.category_id
		WHERE tb.event_id = $1
		  AND t.status NOT IN ('cancelled', 'transferred')
		  AND o.status = 'paid'
		ORDER BY u.full_name ASC
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []comunicadoRecipient
	for rows.Next() {
		var r comunicadoRecipient
		if err := rows.Scan(
			&r.ID, &r.FullName, &r.Email,
			&r.TicketType, &r.PaymentStatus,
			&r.BatchName, &r.CategoryName,
		); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func applyFilters(recipients []comunicadoRecipient, filters []comunicadoFilter) []comunicadoRecipient {
	if len(filters) == 0 {
		return recipients
	}
	var result []comunicadoRecipient
	for _, r := range recipients {
		if matchesAllFilters(r, filters) {
			result = append(result, r)
		}
	}
	return result
}

func matchesAllFilters(r comunicadoRecipient, filters []comunicadoFilter) bool {
	for _, f := range filters {
		var fieldVal string
		switch f.Type {
		case "ticket_type":
			fieldVal = r.TicketType
		case "payment_status":
			fieldVal = r.PaymentStatus
		case "batch_name":
			fieldVal = r.BatchName
		case "category_name":
			if r.CategoryName != nil {
				fieldVal = *r.CategoryName
			}
		default:
			continue
		}
		if !strings.EqualFold(fieldVal, f.Value) {
			return false
		}
	}
	return true
}

func deduplicateByEmail(recipients []comunicadoRecipient) []comunicadoRecipient {
	seen   := make(map[string]struct{}, len(recipients))
	result := make([]comunicadoRecipient, 0, len(recipients))
	for _, r := range recipients {
		key := strings.ToLower(r.Email)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, r)
	}
	return result
}

func extractFromAddress(from string) string {
	start := strings.Index(from, "<")
	end   := strings.Index(from, ">")
	if start != -1 && end != -1 && end > start {
		return from[start+1 : end]
	}
	return from
}

func buildEventInfoBlock(ev eventEmailData) string {
	var rows strings.Builder

	if ev.Date != "" {
		rows.WriteString(fmt.Sprintf(`
			<tr>
			  <td style="padding:4px 0;font-size:12px;color:#5C5C52;">
			    <span style="margin-right:6px;">📅</span>%s
			  </td>
			</tr>`, ev.Date))
	}

	location := strings.TrimSpace(strings.Join(filterEmpty(ev.Venue, ev.City, ev.State), ", "))
	if location != "" {
		rows.WriteString(fmt.Sprintf(`
			<tr>
			  <td style="padding:4px 0;font-size:12px;color:#5C5C52;">
			    <span style="margin-right:6px;">📍</span>%s
			  </td>
			</tr>`, location))
	}

	if rows.Len() == 0 {
		return ""
	}

	return fmt.Sprintf(`
		<table cellpadding="0" cellspacing="0" style="width:100%%;background:#F7F7F2;border-radius:10px;padding:12px 14px;margin-bottom:20px;">
		  %s
		</table>`, rows.String())
}

func filterEmpty(vals ...string) []string {
	var result []string
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			result = append(result, v)
		}
	}
	return result
}

func wrapComunicadoHTML(body, senderName, replyTo string, ev eventEmailData) string {
	footer := ""
	if replyTo != "" {
		footer = fmt.Sprintf(
			`<p style="font-size:11px;color:#9A9A8F;margin-top:24px;border-top:1px solid #F0F0EB;padding-top:12px;">
				Responda para <a href="mailto:%s" style="color:#0A0A0A;">%s</a>
			</p>`, replyTo, replyTo,
		)
	}

	bannerBlock := ""
	if ev.ImageURL != "" {
		bannerBlock = fmt.Sprintf(
			`<tr><td style="height:160px;overflow:hidden;">
				<img src="%s" alt="%s" style="width:100%%;height:160px;object-fit:cover;display:block;" />
			</td></tr>`, ev.ImageURL, ev.Title,
		)
	}

	eventInfoBlock := buildEventInfoBlock(ev)

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#F0F0EB;font-family:'Plus Jakarta Sans',sans-serif;">
  <table width="100%%" cellpadding="0" cellspacing="0" style="padding:32px 16px;">
    <tr><td align="center">
      <table width="100%%" cellpadding="0" cellspacing="0" style="max-width:560px;background:#ffffff;border-radius:20px;border:1px solid #E0E0D8;overflow:hidden;">
        <tr>
          <td style="background:#0A0A0A;padding:20px 32px;">
            <img src="%s" alt="Reppy" style="height:28px;display:block;" />
          </td>
        </tr>
        %s
        <tr>
          <td style="padding:28px 32px;">
            <p style="font-size:11px;color:#9A9A8F;text-transform:uppercase;letter-spacing:2px;margin:0 0 4px;">Enviado por</p>
            <p style="font-size:14px;font-weight:700;color:#0A0A0A;margin:0 0 20px;">%s</p>
            %s
            <div style="font-size:14px;color:#0A0A0A;line-height:1.7;">%s</div>
            %s
          </td>
        </tr>
        <tr>
          <td style="background:#F7F7F2;padding:14px 32px;border-top:1px solid #F0F0EB;text-align:center;">
            <p style="font-size:10px;color:#9A9A8F;margin:0;">Enviado via <strong>Reppy</strong></p>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, reppyLogoURL, bannerBlock, senderName, eventInfoBlock, body, footer)
}