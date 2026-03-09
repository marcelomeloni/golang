// Package ticketpdf centraliza a geração de PDFs de ingressos via chromedp.
// É usado tanto pelo endpoint de download quanto pelo envio de e-mail pós-compra.
package ticketpdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/skip2/go-qrcode"
)

const (
	templatePath = "templates/ticket.html"

	pdfPaperWidth  = 8.27  // A4 em polegadas
	pdfPaperHeight = 11.69
	pdfRenderDelay = time.Second
	pdfTimeout     = 20 * time.Second
)

// EventData contém os dados do evento exibidos no ingresso.
type EventData struct {
	Nome         string
	Data         string
	Hora         string
	VenueName    string
	Street       string
	Number       string
	Neighborhood string
	City         string
	State        string
	CEP          string
	ImageURL     string
}

// TicketData contém todos os dados necessários para renderizar um ingresso em PDF.
// Os campos espelham o contrato do template templates/ticket.html.
type TicketData struct {
	ID       string
	QRCode   string
	LoteName string
	Status   string
	Evento   EventData
}

// templateInput é a struct passada ao html/template — inclui o QR em base64.
type templateInput struct {
	TicketData
	QRBase64 string
}

// Generate renderiza o template HTML com os dados do ingresso e converte para PDF.
// Retorna os bytes do PDF prontos para ser anexados ou enviados ao cliente.
func Generate(data TicketData) ([]byte, error) {
	qrBase64, err := encodeQR(data.QRCode)
	if err != nil {
		return nil, fmt.Errorf("ticketpdf: gerar QR para %s: %w", data.QRCode, err)
	}

	html, err := renderHTML(templateInput{TicketData: data, QRBase64: qrBase64})
	if err != nil {
		return nil, fmt.Errorf("ticketpdf: renderizar HTML: %w", err)
	}

	pdf, err := htmlToPDF(html)
	if err != nil {
		return nil, fmt.Errorf("ticketpdf: converter para PDF: %w", err)
	}

	return pdf, nil
}

// ──────────────────────────────────────────────
// Internals
// ──────────────────────────────────────────────

func encodeQR(code string) (string, error) {
	png, err := qrcode.Encode(code, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(png), nil
}

func renderHTML(data templateInput) (string, error) {
	tmplBytes, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("ler %s: %w", templatePath, err)
	}

	tmpl, err := template.New("ticket").Parse(string(tmplBytes))
	if err != nil {
		return "", fmt.Errorf("parse do template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executar template: %w", err)
	}

	return buf.String(), nil
}

func htmlToPDF(htmlContent string) ([]byte, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var pdfBuf []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			tree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(tree.Frame.ID, htmlContent).Do(ctx)
		}),
		chromedp.Sleep(1*time.Second),
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
