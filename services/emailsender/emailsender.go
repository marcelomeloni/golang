package emailsender

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	resendAPI        = "https://api.resend.com/emails"
	sendRatePerSec   = 2
	rateLimitPause   = time.Second
)

type Recipient struct {
	Name  string
	Email string
}

type Attachment struct {
	Filename    string
	Content     []byte
	ContentType string
}

type Message struct {
	From        string
	To          []Recipient
	Subject     string
	HTMLBody    string
	Variables   map[string]string
	Attachments []Attachment
}

type Result struct {
	ID    string
	Email string
}

type Sender interface {
	Send(msg Message) ([]Result, error)
}

type resendSender struct {
	apiKey string
	client *http.Client
}

func New(apiKey string) Sender {
	if apiKey == "" {
		apiKey = os.Getenv("RESEND_API_KEY")
	}
	return &resendSender{apiKey: apiKey, client: &http.Client{}}
}

func (s *resendSender) Send(msg Message) ([]Result, error) {
	if len(msg.To) == 0 {
		return nil, fmt.Errorf("emailsender: nenhum destinatário informado")
	}

	results := make([]Result, 0, len(msg.To))
	var lastErr error

	for i, r := range msg.To {
		if i > 0 && i%sendRatePerSec == 0 {
			time.Sleep(rateLimitPause)
		}

		html := applyVariables(msg.HTMLBody, r, msg.Variables)

		payload := map[string]interface{}{
			"from":    msg.From,
			"to":      []string{r.Email},
			"subject": msg.Subject,
			"html":    html,
		}

		if len(msg.Attachments) > 0 {
			payload["attachments"] = buildAttachments(msg.Attachments)
		}

		id, err := s.post(payload)
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

func buildAttachments(attachments []Attachment) []map[string]string {
	out := make([]map[string]string, len(attachments))
	for i, a := range attachments {
		out[i] = map[string]string{
			"filename": a.Filename,
			"content":  base64.StdEncoding.EncodeToString(a.Content),
		}
	}
	return out
}

func applyVariables(html string, r Recipient, vars map[string]string) string {
	result := bytes.ReplaceAll([]byte(html), []byte("{{NOME}}"), []byte(r.Name))
	result = bytes.ReplaceAll(result, []byte("{{EMAIL}}"), []byte(r.Email))
	for k, v := range vars {
		result = bytes.ReplaceAll(result, []byte("{{"+k+"}}"), []byte(v))
	}
	return string(result)
}