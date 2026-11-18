package paymentservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type c6WebhookRaw struct {
	WebhookURL string         `json:"webhookUrl"`
	Chave      string         `json:"chave"`
	Criacao    *time.Time     `json:"criacao"`
	AllOf      []c6WebhookRaw `json:"allOf"`
}

// info resolve o webhook independente de o C6 retornar o objeto direto
// ou aninhado dentro de um array "allOf" (formato OpenAPI inline).
func (r c6WebhookRaw) info() WebhookInfo {
	// Percorre allOf até encontrar um objeto com dados reais
	for r.WebhookURL == "" && r.Chave == "" && len(r.AllOf) > 0 {
		r = r.AllOf[0]
	}
	return WebhookInfo{
		WebhookURL: r.WebhookURL,
		Chave:      r.Chave,
		Criacao:    r.Criacao,
	}
}

type c6WebhookListRaw struct {
	Parametros struct {
		Inicio    string `json:"inicio"`
		Fim       string `json:"fim"`
		Paginacao struct {
			PaginaAtual            int `json:"paginaAtual"`
			ItensPorPagina         int `json:"itensPorPagina"`
			QuantidadeDePaginas    int `json:"quantidadeDePaginas"`
			QuantidadeTotalDeItens int `json:"quantidadeTotalDeItens"`
		} `json:"paginacao"`
	} `json:"parametros"`
	Webhooks []c6WebhookRaw `json:"webhooks"`
}

func (g *c6Gateway) Environment() string {
	if g.authURL == c6AuthProdURL {
		return "production"
	}
	return "sandbox"
}

func (g *c6Gateway) PixKey() string {
	return g.pixKey
}

func (g *c6Gateway) GetWebhook(chave string) (WebhookInfo, error) {
	body, status, err := g.doWebhookRequest(http.MethodGet, "/webhook/"+url.PathEscape(chave), nil)
	if err != nil {
		if status == http.StatusNotFound {
			return WebhookInfo{}, ErrWebhookNotFound
		}
		return WebhookInfo{}, err
	}

	var raw c6WebhookRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return WebhookInfo{}, fmt.Errorf("c6bank parse webhook: %w", err)
	}

	info := raw.info()
	if info.Chave == "" {
		info.Chave = chave
	}
	return info, nil
}

func (g *c6Gateway) SetWebhook(chave, webhookURL string) (WebhookInfo, error) {
	payload := map[string]string{"webhookUrl": webhookURL}

	body, _, err := g.doWebhookRequest(http.MethodPut, "/webhook/"+url.PathEscape(chave), payload)
	if err != nil {
		return WebhookInfo{}, err
	}

	if len(bytes.TrimSpace(body)) > 0 {
		var raw c6WebhookRaw
		if err := json.Unmarshal(body, &raw); err == nil {
			info := raw.info()
			if info.WebhookURL != "" || info.Chave != "" {
				if info.Chave == "" {
					info.Chave = chave
				}
				if info.WebhookURL == "" {
					info.WebhookURL = webhookURL
				}
				return info, nil
			}
		}
	}

	return WebhookInfo{WebhookURL: webhookURL, Chave: chave}, nil
}

func (g *c6Gateway) DeleteWebhook(chave string) error {
	_, status, err := g.doWebhookRequest(http.MethodDelete, "/webhook/"+url.PathEscape(chave), nil)
	if err != nil {
		if status == http.StatusNotFound {
			return ErrWebhookNotFound
		}
		return err
	}
	return nil
}

func (g *c6Gateway) ListWebhooks(params WebhookListParams) (WebhookList, error) {
	query := url.Values{}
	if params.Inicio != nil {
		query.Set("inicio", params.Inicio.UTC().Format(time.RFC3339))
	}
	if params.Fim != nil {
		query.Set("fim", params.Fim.UTC().Format(time.RFC3339))
	}
	// Sempre envia paginação — C6 pode retornar vazio sem esses parâmetros
	query.Set("paginacao.paginaAtual", fmt.Sprintf("%d", params.PaginaAtual))
	if params.ItensPorPagina > 0 {
		query.Set("paginacao.itensPorPagina", fmt.Sprintf("%d", params.ItensPorPagina))
	} else {
		query.Set("paginacao.itensPorPagina", "100")
	}

	path := "/webhook"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	body, _, err := g.doWebhookRequest(http.MethodGet, path, nil)
	if err != nil {
		return WebhookList{}, err
	}

	log.Printf("C6 ListWebhooks RAW: %s", string(body))

	var raw c6WebhookListRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return WebhookList{}, fmt.Errorf("c6bank parse webhooks: %w", err)
	}

	log.Printf("C6 ListWebhooks parsed: %d webhooks, totalItens=%d",
		len(raw.Webhooks), raw.Parametros.Paginacao.QuantidadeTotalDeItens)

	result := WebhookList{
		Webhooks: make([]WebhookInfo, 0, len(raw.Webhooks)),
		Parametros: WebhookListParameters{
			Inicio: raw.Parametros.Inicio,
			Fim:    raw.Parametros.Fim,
			Paginacao: WebhookPagination{
				PaginaAtual:          raw.Parametros.Paginacao.PaginaAtual,
				ItensPorPagina:       raw.Parametros.Paginacao.ItensPorPagina,
				QuantidadeDePaginas:  raw.Parametros.Paginacao.QuantidadeDePaginas,
				QuantidadeTotalItens: raw.Parametros.Paginacao.QuantidadeTotalDeItens,
			},
		},
	}
	for _, item := range raw.Webhooks {
		result.Webhooks = append(result.Webhooks, item.info())
	}

	return result, nil
}

func (g *c6Gateway) doWebhookRequest(method, path string, payload interface{}) ([]byte, int, error) {
	return g.doC6Request(method, path, payload)
}

func (g *c6Gateway) doC6Request(method, path string, payload interface{}) ([]byte, int, error) {
	token, err := g.getToken()
	if err != nil {
		return nil, 0, err
	}

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequest(method, g.pixBaseURL+path, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("c6bank webhook %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &C6APIError{Status: resp.StatusCode}
		if len(bytes.TrimSpace(respBody)) > 0 {
			if jsonErr := json.Unmarshal(respBody, apiErr); jsonErr != nil {
				apiErr.Detail = strings.TrimSpace(string(respBody))
			} else if apiErr.Detail == "" && apiErr.Title == "" {
				apiErr.Detail = strings.TrimSpace(string(respBody))
			}
		}
		if apiErr.Detail == "" && apiErr.Title == "" {
			apiErr.Detail = fmt.Sprintf("C6 retornou HTTP %d", resp.StatusCode)
		}
		return respBody, resp.StatusCode, apiErr
	}

	return respBody, resp.StatusCode, nil
}
