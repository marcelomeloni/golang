package emailsender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const resendAPI = "https://api.resend.com/emails"

// ──────────────────────────────────────────────
// Tipos públicos
// ──────────────────────────────────────────────

// Recipient representa um destinatário de e-mail.
type Recipient struct {
	Name  string // usado para substituição de {{NOME}} no template
	Email string
}

// Message contém tudo necessário para enviar um e-mail.
type Message struct {
	From      string            // ex: "Reppy <noreply@reppy.com.br>"
	To        []Recipient
	Subject   string
	HTMLBody  string            // HTML já renderizado (pode conter {{CHAVE}})
	Variables map[string]string // variáveis extras substituídas no HTML
}

// Result é retornado após o envio de cada e-mail.
type Result struct {
	ID    string
	Email string
}

// Sender é a interface que o resto da aplicação usa.
// Facilita mock em testes e troca de provider no futuro.
type Sender interface {
	Send(msg Message) ([]Result, error)
}

// ──────────────────────────────────────────────
// Implementação Resend
// ──────────────────────────────────────────────

type resendSender struct {
	apiKey string
	client *http.Client
}

// New cria um Sender via Resend. Se apiKey for vazio, lê RESEND_API_KEY do env.
func New(apiKey string) Sender {
	if apiKey == "" {
		apiKey = os.Getenv("RESEND_API_KEY")
	}
	return &resendSender{apiKey: apiKey, client: &http.Client{}}
}

// Send envia para cada destinatário individualmente, permitindo personalização por pessoa.
func (s *resendSender) Send(msg Message) ([]Result, error) {
	if len(msg.To) == 0 {
		return nil, fmt.Errorf("emailsender: nenhum destinatário informado")
	}

	results := make([]Result, 0, len(msg.To))
	var lastErr error

	for _, r := range msg.To {
		html := applyVariables(msg.HTMLBody, r, msg.Variables)

		id, err := s.post(map[string]interface{}{
			"from":    msg.From,
			"to":      []string{r.Email},
			"subject": msg.Subject,
			"html":    html,
		})
		if err != nil {
			lastErr = fmt.Errorf("emailsender: %s: %w", r.Email, err)
			continue
		}
		results = append(results, Result{ID: id, Email: r.Email})
	}

	return results, lastErr
}

func (s *resendSender) post(payload map[string]interface{}) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest(http.MethodPost, resendAPI, bytes.NewBuffer(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var body struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("resposta inválida da API: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("resend %d: %s", resp.StatusCode, body.Message)
	}

	return body.ID, nil
}

// applyVariables substitui {{NOME}}, {{EMAIL}} e as chaves de vars no HTML.
func applyVariables(html string, r Recipient, vars map[string]string) string {
	result := bytes.ReplaceAll([]byte(html), []byte("{{NOME}}"), []byte(r.Name))
	result = bytes.ReplaceAll(result, []byte("{{EMAIL}}"), []byte(r.Email))
	for k, v := range vars {
		result = bytes.ReplaceAll(result, []byte("{{"+k+"}}"), []byte(v))
	}
	return string(result)
}