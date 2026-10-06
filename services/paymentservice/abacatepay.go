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
	abacateV2Base   = "https://api.abacatepay.com/v2"
	pixExpiresInSec = 900
)

type abacateCustomer struct {
	Name      string `json:"name"`
	Cellphone string `json:"cellphone"`
	Email     string `json:"email"`
	TaxID     string `json:"taxId"`
}

type abacateCreateRequest struct {
	Amount      int              `json:"amount"`
	ExpiresIn   int              `json:"expiresIn"`
	Description string           `json:"description,omitempty"`
	Customer    *abacateCustomer `json:"customer,omitempty"`
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

type abacateWithdrawPixData struct {
	Key  string `json:"key"`
	Type string `json:"type"`
}

type abacateWithdrawRequest struct {
	ExternalID  string                 `json:"externalId"`
	Method      string                 `json:"method"`
	Amount      int                    `json:"amount"`
	Pix         abacateWithdrawPixData `json:"pix"`
	Description string                 `json:"description,omitempty"`
}

type abacateWithdrawResponse struct {
	Data  any `json:"data"`
	Error any `json:"error"`
}

type abacateRefundRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type abacateRefundResponse struct {
	Data any `json:"data"`
	// Error vem como string (ex.: "TRANSACTION_NOT_REFUNDABLE") ou null.
	Error   any    `json:"error"`
	Success bool   `json:"success"`
}

type abacateGateway struct {
	apiKey     string
	httpClient *http.Client
}

func NewAbacatePay(apiKey string) Gateway {
	return &abacateGateway{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

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
			phone = "(00) 00000-0000"
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

// Withdraw envia um pagamento PIX ao vendedor.
// pixKeyType deve ser um dos valores: cpf, email, phone, random
// A API do AbacatePay exige o type em uppercase: CPF, EMAIL, PHONE, RANDOM
func (g *abacateGateway) Withdraw(referenceID string, amountBRL float64, pixKey string, pixKeyType string) error {
	payload := abacateWithdrawRequest{
		ExternalID:  fmt.Sprintf("withdraw-%s", referenceID),
		Method:      "PIX",
		Amount:      int(amountBRL * 100),
		Pix: abacateWithdrawPixData{
			Key:  pixKey,
			Type: strings.ToUpper(pixKeyType),
		},
		Description: fmt.Sprintf("Reppy Market - venda %s", referenceID),
	}

	respData, err := g.post("/withdraw/create", payload)
	if err != nil {
		return fmt.Errorf("abacatepay withdraw: %w", err)
	}

	var parsed abacateWithdrawResponse
	if err := json.Unmarshal(respData, &parsed); err != nil {
		return fmt.Errorf("abacatepay withdraw parse: %w", err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("abacatepay withdraw api error: %v", parsed.Error)
	}

	return nil
}

// Refund estorna integralmente uma cobrança já paga.
//
// A API v1 da AbacatePay não expõe estorno (só notifica via webhook
// `billing.refunded`, disparado quando o estorno é feito no painel deles).
// Usamos o endpoint v2 `POST /checkouts/refund`, que aceita o mesmo
// `pix_char_...` gerado em `/pixQrCode/create`.
//
// Regras da AbacatePay: reembolso apenas total, transação precisa estar paga,
// valor debitado do saldo da loja. Estornar duas vezes não cria outro
// reembolso — tratamos esse caso como sucesso (idempotente).
func (g *abacateGateway) Refund(externalID, reason string) error {
	payload := abacateRefundRequest{ID: externalID, Reason: reason}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("abacatepay refund marshal: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, abacateV2Base+"/checkouts/refund", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	g.setAuthHeader(req)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("abacatepay refund: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("abacatepay refund read: %w", err)
	}

	var parsed abacateRefundResponse
	_ = json.Unmarshal(respBody, &parsed)

	if resp.StatusCode == http.StatusOK && parsed.Success {
		return nil
	}

	// Já reembolsada → nada a fazer (idempotência).
	if resp.StatusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(string(respBody)), "reembolsada") {
		return nil
	}

	return fmt.Errorf("abacatepay refund status %d: %s", resp.StatusCode, respBody)
}

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