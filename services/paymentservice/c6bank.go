package paymentservice

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

const (
	c6AuthSandboxURL   = "https://baas-api-sandbox.c6bank.info/v1/auth"
	c6AuthProdURL      = "https://baas-api.c6bank.info/v1/auth"
	c6PixSandboxURL    = "https://baas-api-sandbox.c6bank.info/v2/pix"
	c6PixProdURL       = "https://baas-api.c6bank.info/v2/pix"
	c6PixExpiresInSec  = 900
)

type c6TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

type c6CobSolicitada struct {
	Calendario struct {
		Expiracao int `json:"expiracao"`
	} `json:"calendario"`
	Valor struct {
		Original string `json:"original"`
	} `json:"valor"`
	Chave      string          `json:"chave"`
	SolicitacaoPagador string   `json:"solicitacaoPagador,omitempty"`
	Devedor    *c6Pessoa      `json:"devedor,omitempty"`
	InfoAdicionais []map[string]string `json:"infoAdicionais,omitempty"`
}

type c6Pessoa struct {
	Nome string `json:"nome"`
	CPF  string `json:"cpf,omitempty"`
	CNPJ string `json:"cnpj,omitempty"`
}

type c6CobGerada struct {
	Txid      string    `json:"txid"`
	Status    string    `json:"status"`
	Calendario struct {
		Criacao   time.Time `json:"criacao"`
		Expiracao int       `json:"expiracao"`
		Apresentacao time.Time `json:"apresentacao,omitempty"`
	} `json:"calendario"`
	Valor struct {
		Original string `json:"original"`
	} `json:"valor"`
	Chave      string `json:"chave"`
	Location   string `json:"location"`
	PixCopiaECola string `json:"pixCopiaECola"`
	QrCode     struct {
		Imagem string `json:"imagem,omitempty"`
		Emv    string `json:"emv,omitempty"`
	} `json:"qrCode,omitempty"`
}

type c6Gateway struct {
	httpClient *http.Client
	authURL    string
	pixBaseURL string
	clientID   string
	clientSecret string
	pixKey     string

	mu          sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

func NewC6Bank(clientID, clientSecret, pixKey, env string) (Gateway, error) {
	env = strings.ToLower(strings.TrimSpace(env))
	var authURL, pixBaseURL string
	if env == "production" || env == "prod" {
		authURL = c6AuthProdURL
		pixBaseURL = c6PixProdURL
	} else {
		authURL = c6AuthSandboxURL
		pixBaseURL = c6PixSandboxURL
	}

	certPath := os.Getenv("C6_CERT_PATH")
	keyPath := os.Getenv("C6_KEY_PATH")
	if certPath == "" {
		certPath = "certs/c6_client.crt"
	}
	if keyPath == "" {
		keyPath = "certs/c6_client.key"
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("c6bank: carregar certificado: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	g := &c6Gateway{
		httpClient:   client,
		authURL:      authURL,
		pixBaseURL:   pixBaseURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		pixKey:       pixKey,
	}
	return g, nil
}

func (g *c6Gateway) getToken() (string, error) {
	g.mu.RLock()
	if g.accessToken != "" && time.Now().Before(g.tokenExpiry.Add(-30*time.Second)) {
		token := g.accessToken
		g.mu.RUnlock()
		return token, nil
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.accessToken != "" && time.Now().Before(g.tokenExpiry.Add(-30*time.Second)) {
		return g.accessToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", g.clientID)
	form.Set("client_secret", g.clientSecret)

	req, err := http.NewRequest(http.MethodPost, g.authURL+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("c6bank auth: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("c6bank auth status %d: %s", resp.StatusCode, body)
	}

	var tr c6TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("c6bank auth parse: %w", err)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("c6bank auth: token vazio")
	}

	g.accessToken = tr.AccessToken
	if tr.ExpiresIn > 0 {
		g.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	} else {
		g.tokenExpiry = time.Now().Add(10 * time.Minute)
	}
	return g.accessToken, nil
}

func (g *c6Gateway) GeneratePix(orderID string, amountBRL float64, buyerName, buyerEmail, buyerCPF, buyerPhone string) (Result, error) {
	token, err := g.getToken()
	if err != nil {
		return Result{}, err
	}

	// Use txid based on orderID (alphanumeric, 26-35 chars)
	txid := strings.ReplaceAll(orderID, "-", "")
	if len(txid) > 35 {
		txid = txid[:35]
	}
	for len(txid) < 26 {
		txid += "0"
	}

	amount := fmt.Sprintf("%.2f", amountBRL)
	payload := c6CobSolicitada{
		Chave: g.pixKey,
	}
	payload.Calendario.Expiracao = c6PixExpiresInSec
	payload.Valor.Original = amount
	payload.SolicitacaoPagador = fmt.Sprintf("Pedido %s", orderID)

	if buyerName != "" && buyerCPF != "" {
		cleanCPF := strings.ReplaceAll(strings.ReplaceAll(buyerCPF, ".", ""), "-", "")
		if len(cleanCPF) == 11 {
			payload.Devedor = &c6Pessoa{
				Nome: buyerName,
				CPF:  cleanCPF,
			}
		} else if len(cleanCPF) == 14 {
			payload.Devedor = &c6Pessoa{
				Nome: buyerName,
				CNPJ: cleanCPF,
			}
		}
	}

	// Add infoAdicionais with order_id
	payload.InfoAdicionais = []map[string]string{
		{"nome": "order_id", "valor": orderID},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}

	url := fmt.Sprintf("%s/cob/%s", g.pixBaseURL, txid)
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("c6bank create cob: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("c6bank create cob status %d: %s", resp.StatusCode, respBody)
	}

	var cob c6CobGerada
	if err := json.Unmarshal(respBody, &cob); err != nil {
		return Result{}, fmt.Errorf("c6bank parse cob: %w", err)
	}

	pixCode := cob.PixCopiaECola
	if pixCode == "" && cob.QrCode.Emv != "" {
		pixCode = cob.QrCode.Emv
	}

	pixQr := ""
	if cob.QrCode.Imagem != "" {
		img := cob.QrCode.Imagem
		if strings.Contains(img, ",") {
			parts := strings.SplitN(img, ",", 2)
			if len(parts) == 2 {
				pixQr = parts[1]
			}
		} else {
			pixQr = img
		}
	}
	if pixQr == "" && pixCode != "" {
		if png, err := qrcode.Encode(pixCode, qrcode.Medium, 256); err == nil {
			pixQr = base64.StdEncoding.EncodeToString(png)
		}
	}

	return Result{
		PixCode:    pixCode,
		PixQrCode:  pixQr,
		ExternalID: cob.Txid,
	}, nil
}

func (g *c6Gateway) CheckStatus(externalID string) (string, error) {
	token, err := g.getToken()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/cob/%s", g.pixBaseURL, externalID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("c6bank check cob: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("c6bank check cob status %d: %s", resp.StatusCode, body)
	}

	var cob c6CobGerada
	if err := json.Unmarshal(body, &cob); err != nil {
		return "", fmt.Errorf("c6bank parse cob: %w", err)
	}

	// Normalize status to what webhook/logic expects
	status := strings.ToUpper(cob.Status)
	if status == "CONCLUIDA" || status == "PAID" || status == "PAGO" {
		return "PAID", nil
	}
	if status == "ATIVA" || status == "ACTIVE" {
		return "ACTIVE", nil
	}
	return status, nil
}

func (g *c6Gateway) Withdraw(referenceID string, amountBRL float64, pixKey string, pixKeyType string) error {
	// Not implemented for C6 in this migration; return nil to avoid breaking flows if not used
	return nil
}

func (g *c6Gateway) Refund(externalID, reason string) error {
	// Not implemented for C6 in this migration
	return nil
}
