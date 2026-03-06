	package client

	import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64" // Adicionado
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"bilheteria-api/config"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode" // Adicionado
)


	type MyTicketEvent struct {
		Slug     string `json:"slug"`
		Nome     string `json:"nome"`
		Data     string `json:"data"`
		Hora     string `json:"hora"`
		Local    string `json:"local"`
		ImageURL string `json:"imagemUrl"`
	}

	type MyTicket struct {
		ID               string        `json:"id"`
		Status           string        `json:"status"` // "ativo" | "usado" | "encerrado"
		QRCode           string        `json:"qrCode"`
		LoteName         string        `json:"lote"`
		AllowTransfer    bool          `json:"allowTransfer"`
		AllowReppyMarket bool          `json:"allowReppyMarket"`
		Evento           MyTicketEvent `json:"evento"`
	}


	func GetMyTickets(c *gin.Context) {
		userID, exists := c.Get("userID")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
			return
		}

		db := config.GetDB()

		rows, err := db.Query(myTicketsQuery, userID)
		if err != nil {
			log.Printf("GetMyTickets query: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar ingressos"})
			return
		}
		defer rows.Close()

		proximos, passados, err := groupTickets(rows)
		if err != nil {
			log.Printf("GetMyTickets scan: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar ingressos"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"proximos": proximos,
			"passados": passados,
		})
	}


func DownloadTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	ticketID := c.Param("id")
	db := config.GetDB()

	row := db.QueryRow(singleTicketQuery, ticketID, userID)
	ticket, err := scanSingleTicket(row)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não encontrado"})
		return
	}

	// 1. Gera o QR Code em memória (Base64)
	png, err := qrcode.Encode(ticket.QRCode, qrcode.Medium, 256)
	if err != nil {
		c.JSON(500, gin.H{"error": "erro ao gerar QR"})
		return
	}
	qrBase64 := base64.StdEncoding.EncodeToString(png)

	// Dados para o template
	templateData := struct {
		MyTicket
		QRBase64 string
	}{
		MyTicket: ticket,
		QRBase64: qrBase64,
	}

	// 2. Renderiza o HTML
	tmplBytes, err := os.ReadFile("templates/ticket.html")
	if err != nil {
		c.JSON(500, gin.H{"error": "template não encontrado"})
		return
	}

	tmpl, err := template.New("ticket").Parse(string(tmplBytes))
	if err != nil {
		c.JSON(500, gin.H{"error": "erro no parse do template"})
		return
	}

	var htmlBuf bytes.Buffer
	if err := tmpl.Execute(&htmlBuf, templateData); err != nil {
		c.JSON(500, gin.H{"error": "erro ao gerar HTML"})
		return
	}

	// 3. Converte para PDF
	pdfBytes, err := htmlToPDF(htmlBuf.String())
	if err != nil {
		log.Printf("Erro PDF: %v", err)
		c.JSON(500, gin.H{"error": "erro ao gerar PDF"})
		return
	}

	// 4. Envio do arquivo
	fileName := fmt.Sprintf("ingresso-%s.pdf", ticket.QRCode)
	c.Header("Access-Control-Expose-Headers", "Content-Disposition")
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	c.Data(http.StatusOK, "application/pdf", pdfBytes)
}

func htmlToPDF(htmlContent string) ([]byte, error) {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var pdfBuf []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Pega o ID do frame principal para injetar o HTML
			tree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(tree.Frame.ID, htmlContent).Do(ctx)
		}),
		// Espera as fontes carregarem um pouco
		chromedp.Sleep(1 * time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27).
				WithPaperHeight(11.69).
				Do(ctx)
			return err
		}),
	)
	return pdfBuf, err
}

	// ──────────────────────────────────────────────
	// Queries
	// ──────────────────────────────────────────────

	const myTicketsQuery = `
		SELECT
			t.id,
			t.qr_code,
			t.status,
			t.checked_in_at,
			tb.name                               AS lote_name,
			tb.allow_transfer,
			e.slug,
			e.title,
			e.image_url,
			e.start_date,
			e.end_date,
			e.location,
			COALESCE(e.allow_reppy_market, false) AS allow_reppy_market
		FROM tickets t
		JOIN orders          o  ON o.id  = t.order_id
		JOIN events          e  ON e.id  = o.event_id
		LEFT JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.user_id = $1
		AND o.status  = 'paid'
		AND t.status NOT IN ('cancelled')
		ORDER BY e.start_date ASC`

	// singleTicketQuery — mesma projeção, mas filtra pelo id do ticket
	// e valida que o dono é quem está pedindo o download.
	const singleTicketQuery = `
		SELECT
			t.id,
			t.qr_code,
			t.status,
			t.checked_in_at,
			tb.name                               AS lote_name,
			tb.allow_transfer,
			e.slug,
			e.title,
			e.image_url,
			e.start_date,
			e.end_date,
			e.location,
			COALESCE(e.allow_reppy_market, false) AS allow_reppy_market
		FROM tickets t
		JOIN orders          o  ON o.id  = t.order_id
		JOIN events          e  ON e.id  = o.event_id
		LEFT JOIN ticket_batches tb ON tb.id = t.batch_id
		WHERE t.id      = $1
		AND t.user_id = $2
		AND o.status  = 'paid'`

	// ──────────────────────────────────────────────
	// Helpers de scan
	// ──────────────────────────────────────────────

	// groupTickets lê todas as linhas e separa em dois slices:
	// proximos → status "ativo"
	// passados → status "usado" ou "encerrado"
	func groupTickets(rows *sql.Rows) (proximos, passados []MyTicket, err error) {
		loc, _ := time.LoadLocation("America/Sao_Paulo")

		proximos = []MyTicket{}
		passados = []MyTicket{}

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

	func scanSingleTicket(row *sql.Row) (MyTicket, error) {
		loc, _ := time.LoadLocation("America/Sao_Paulo")
		return scanTicketRow(row.Scan, loc)
	}

	// scanTicketRow aceita qualquer função de scan compatível (rows.Scan ou row.Scan),
	// evitando duplicar a lógica de leitura de colunas.
	func scanTicketRow(scan func(...any) error, loc *time.Location) (MyTicket, error) {
		var (
			id               string
			qrCode           string
			dbStatus         string
			checkedInAt      sql.NullTime
			loteName         sql.NullString
			allowTransfer    sql.NullBool
			slug             string
			title            string
			imageURL         sql.NullString
			startDate        sql.NullTime
			endDate          sql.NullTime
			locationJSON     []byte
			allowReppyMarket bool
		)

		err := scan(
			&id, &qrCode, &dbStatus, &checkedInAt,
			&loteName, &allowTransfer,
			&slug, &title, &imageURL,
			&startDate, &endDate, &locationJSON,
			&allowReppyMarket,
		)
		if err != nil {
			return MyTicket{}, err
		}

		return MyTicket{
			ID:               id,
			Status:           resolveClientStatus(dbStatus, checkedInAt, endDate, loc),
			QRCode:           qrCode,
			LoteName:         loteName.String,
			AllowTransfer:    allowTransfer.Bool,
			AllowReppyMarket: allowReppyMarket,
			Evento: MyTicketEvent{
				Slug:     slug,
				Nome:     title,
				Data:     formatDate(startDate, loc),
				Hora:     formatTime(startDate, loc),
				Local:    parseVenue(locationJSON),
				ImageURL: imageURL.String,
			},
		}, nil
	}

	// ──────────────────────────────────────────────
	// Status
	// ──────────────────────────────────────────────

	func resolveClientStatus(
		dbStatus string,
		checkedInAt sql.NullTime,
		endDate sql.NullTime,
		loc *time.Location,
	) string {
		if dbStatus == "used" || checkedInAt.Valid {
			return "usado"
		}
		if dbStatus == "transferred" {
			return "encerrado"
		}
		if endDate.Valid && time.Now().In(loc).After(endDate.Time.In(loc)) {
			return "encerrado"
		}
		return "ativo"
	}

	// ──────────────────────────────────────────────
	// Formatação de data/hora e local
	// ──────────────────────────────────────────────

	var (
		diasSemana = []string{"Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"}
		meses      = []string{"", "Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
	)

	func formatDate(t sql.NullTime, loc *time.Location) string {
		if !t.Valid {
			return "Data a confirmar"
		}
		ev := t.Time.In(loc)
		return fmt.Sprintf("%s, %d %s", diasSemana[ev.Weekday()], ev.Day(), meses[ev.Month()])
	}

	func formatTime(t sql.NullTime, loc *time.Location) string {
		if !t.Valid {
			return ""
		}
		ev := t.Time.In(loc)
		if ev.Minute() == 0 {
			return fmt.Sprintf("%dh", ev.Hour())
		}
		return fmt.Sprintf("%dh%02d", ev.Hour(), ev.Minute())
	}

	func parseVenue(locationJSON []byte) string {
		if len(locationJSON) == 0 {
			return "Local a definir"
		}
		var l Location
		if err := json.Unmarshal(locationJSON, &l); err != nil {
			return "Local a definir"
		}
		switch {
		case l.VenueName != "" && l.City != "":
			return fmt.Sprintf("%s, %s", l.VenueName, l.City)
		case l.VenueName != "":
			return l.VenueName
		case l.City != "":
			return l.City
		default:
			return "Local a definir"
		}
	}