package emailtemplates

import (
	"fmt"
	"os"
	"strings"
)

const eventCancelledTemplatePath = "templates/event_cancelled.html"

// EventCancelledData contém os dados do e-mail de evento cancelado.
type EventCancelledData struct {
	NomeUsuario  string
	EventoNome   string
	EventoData   string
	EventoHora   string
	EventoLocal  string
	ReembolsoNote string
}

// BuildEventCancelled lê o template do evento cancelado e retorna o subject e o
// HTML prontos para envio.
func BuildEventCancelled(d EventCancelledData) (subject, html string, err error) {
	tmplBytes, err := os.ReadFile(eventCancelledTemplatePath)
	if err != nil {
		return "", "", fmt.Errorf("emailtemplates: ler template de cancelamento: %w", err)
	}

	subject = fmt.Sprintf("⚠️ %s foi cancelado", d.EventoNome)

	r := strings.NewReplacer(
		"{{NOME}}",           d.NomeUsuario,
		"{{EVENTO_NOME}}",    d.EventoNome,
		"{{EVENTO_DATA}}",    d.EventoData,
		"{{EVENTO_HORA}}",    d.EventoHora,
		"{{EVENTO_LOCAL}}",   d.EventoLocal,
		"{{REEMBOLSO_NOTE}}", d.ReembolsoNote,
	)

	return subject, r.Replace(string(tmplBytes)), nil
}