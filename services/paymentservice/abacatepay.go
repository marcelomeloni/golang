package paymentservice

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	abacateBaseURL  = "https://api.abacatepay.com/v1"
	pixExpiresInSec = 3600 // 1 hora
)

// ── Request / Response DTOs ───────────────────────────────────────────────────

type abacateCustomer struct {
	Name      string `json:"name"`
	Cellphone string `json:"cellphone"`
	Email     string `json:"email"`
	TaxID     string `json:"taxId"`
}

type abacateCreateRequest struct {
	Amount      int              `json:"amount"`             // centavos
	ExpiresIn   int              `json:"expiresIn"`
	Description string           `json:"description,omitempty"`
	Customer    *abacateCustomer `json:"customer,omitempty"` // nil → omitido no JSON
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

type abacateCreateResponse struct {
	Data struct {
		ID           string    `json:"id"`
		BrCode       string    `json:"brCode"`
		BrCodeBase64 string    `json:"brCodeBase64"`
		Status       string    `json:"status"`
		ExpiresAt    time.Time `json:"expiresAt"`
	} `json:"data"`
	Error any `json:"error"`
}

type abacateCheckResponse struct {
	Data struct {
		Status    string    `json:"status"`
		ExpiresAt time.Time `json:"expiresAt"`
	} `json:"data"`
	Error any `json:"error"`
}

// ── Gateway ───────────────────────────────────────────────────────────────────

type abacateGateway struct {
	apiKey     string
	httpClient *http.Client
}

// NewAbacatePay retorna um Gateway configurado para o AbacatePay.
func NewAbacatePay(apiKey string) Gateway {
	return &abacateGateway{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// GeneratePix cria um QRCode Pix no AbacatePay.
// O objeto customer só é enviado quando name, email e cpf estão presentes —
// a API exige todos os 4 campos ou nenhum.
func (g *abacateGateway) GeneratePix(
	orderID string,
	amountBRL float64,
	buyerName, buyerEmail, buyerCPF, buyerPhone string,
) (Result, error) {
	payload := abacateCreateRequest{
		Amount:    int(amountBRL * 100),
		ExpiresIn: pixExpiresInSec,
		Metadata:  map[string]any{"order_id": orderID},
	}

	if buyerName != "" && buyerEmail != "" && buyerCPF != "" {
		phone := strings.TrimSpace(buyerPhone)
		if phone == "" {
			phone = "(00) 00000-0000" // placeholder: API exige o campo mas não valida o número
		}
		payload.Customer = &abacateCustomer{
			Name:      buyerName,
			Email:     buyerEmail,
			TaxID:     buyerCPF,
			Cellphone: phone,
		}
	}

	respData, err := g.post("/pixQrCode/create", payload)
	if err != nil {
		return Result{}, fmt.Errorf("abacatepay create pix: %w", err)
	}

	var parsed abacateCreateResponse
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return Result{}, fmt.Errorf("abacatepay parse response: %w", err)
	}
	if parsed.Error != nil {
		return Result{}, fmt.Errorf("abacatepay api error: %v", parsed.Error)
	}

	return Result{
		PixCode:    parsed.Data.BrCode,
		PixQrCode:  parsed.Data.BrCodeBase64,
		ExternalID: parsed.Data.ID,
	}, nil
}

// CheckStatus consulta o status atual de uma cobrança Pix pelo ID externo.
func (g *abacateGateway) CheckStatus(externalID string) (string, error) {
	url := fmt.Sprintf("%s/pixQrCode/check?id=%s", abacateBaseURL, externalID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	g.setAuthHeader(req)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("abacatepay check status: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("abacatepay check status %d: %s", resp.StatusCode, body)
	}

	var parsed abacateCheckResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("abacatepay parse check: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("abacatepay api error: %v", parsed.Error)
	}

	return parsed.Data.Status, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (g *abacateGateway) post(path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, abacateBaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	g.setAuthHeader(req)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	return respBody, nil
}

func (g *abacateGateway) setAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
}