package client

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS
// ==========================================

type RegisterRequest struct {
	FullName string `json:"fullName" binding:"required"`
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type RegisterResponse struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	AccessToken string `json:"accessToken,omitempty"`
	Message     string `json:"message"`
}

type LoginRequest struct {
	Identifier string `json:"identifier" binding:"required"` // e-mail OU cpf
	Password   string `json:"password"   binding:"required"`
}

type LoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       string `json:"userId"`
	Email        string `json:"email"`
	FullName     string `json:"fullName"`
}

// ==========================================
// CONFIG SUPABASE
//
// Necessário no .env / ambiente:
//   SUPABASE_URL       = https://SEU-PROJETO.supabase.co
//   SUPABASE_ANON_KEY  = a chave "anon public" do projeto
// ==========================================

func supabaseURL() string {
	return strings.TrimRight(os.Getenv("SUPABASE_URL"), "/")
}

func supabaseAnonKey() string {
	return os.Getenv("SUPABASE_ANON_KEY")
}

// ==========================================
// HELPERS — chamadas REST ao Supabase Auth
// (evita dependência de SDK; usa apenas net/http)
// ==========================================

type supabaseErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Msg              string `json:"msg"`
	Message          string `json:"message"`
}

func supabaseErrMessage(body []byte) string {
	var e supabaseErrorResponse
	if err := json.Unmarshal(body, &e); err == nil {
		switch {
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Msg != "":
			return e.Msg
		case e.Message != "":
			return e.Message
		case e.Error != "":
			return e.Error
		}
	}
	return string(body)
}

type supabaseUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type supabaseAuthResponse struct {
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	User         *supabaseUser `json:"user"`
	// Se a confirmação de e-mail estiver habilitada no projeto Supabase,
	// o signup retorna apenas "user", sem access_token/refresh_token.
}

func supabaseSignUp(email, password, fullName string) (*supabaseAuthResponse, int, error) {
	payload := map[string]interface{}{
		"email":    email,
		"password": password,
		"data": map[string]interface{}{
			"full_name": fullName,
		},
	}
	b, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, supabaseURL()+"/auth/v1/signup", bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseAnonKey())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("%s", supabaseErrMessage(respBody))
	}

	var out supabaseAuthResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
}

func supabaseSignInWithPassword(email, password string) (*supabaseAuthResponse, int, error) {
	b, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	req, err := http.NewRequest(http.MethodPost, supabaseURL()+"/auth/v1/token?grant_type=password", bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", supabaseAnonKey())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("%s", supabaseErrMessage(respBody))
	}

	var out supabaseAuthResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return &out, resp.StatusCode, nil
}

// ==========================================
// HANDLERS
// ==========================================

// Register — cria a conta no Supabase Auth (e-mail + senha) e espelha
// o perfil básico (id, email, full_name) na tabela public.users.
// O restante do perfil (CPF, telefone, data de nascimento) continua
// sendo preenchido depois via POST /client/auth/complete-profile.
func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	fullName := strings.TrimSpace(req.FullName)

	authResp, status, err := supabaseSignUp(email, req.Password, fullName)
	if err != nil {
		if status == http.StatusUnprocessableEntity || status == http.StatusBadRequest {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Este e-mail já está cadastrado.",
				"code":  "email_conflict",
			})
			return
		}
		log.Printf("Erro no signup Supabase: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao criar conta: " + err.Error()})
		return
	}

	if authResp.User == nil || authResp.User.ID == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Resposta inesperada do serviço de autenticação"})
		return
	}

	db := config.GetDB()
	if _, err := db.Exec(`
		INSERT INTO users (id, email, full_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE
		SET email = EXCLUDED.email, full_name = EXCLUDED.full_name, updated_at = NOW()
	`, authResp.User.ID, email, fullName); err != nil {
		// A conta de auth já foi criada; não falha a resposta por causa
		// disso, mas loga para investigação. O front pode reenviar via
		// /complete-profile se algo faltar.
		log.Printf("Erro ao gravar perfil pós-cadastro: %v", err)
	}

	resp := RegisterResponse{
		UserID:  authResp.User.ID,
		Email:   email,
		Message: "Conta criada com sucesso.",
	}
	if authResp.AccessToken != "" {
		resp.AccessToken = authResp.AccessToken
	} else {
		resp.Message = "Conta criada. Verifique seu e-mail para confirmar o cadastro."
	}

	c.JSON(http.StatusCreated, resp)
}

// Login — autentica por E-MAIL ou CPF + senha.
// Se o identificador não contiver "@", ele é tratado como CPF: o e-mail
// correspondente é resolvido em public.users antes de chamar o Supabase Auth.
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	identifier := strings.TrimSpace(req.Identifier)
	email := ""

	if strings.Contains(identifier, "@") {
		email = strings.ToLower(identifier)
	} else {
		cpfDigits, ok := cleanCPF(identifier)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Informe um e-mail válido ou um CPF com 11 dígitos."})
			return
		}

		db := config.GetDB()
		var foundEmail sql.NullString
		err := db.QueryRow(`
			SELECT email FROM users WHERE cpf_digits = $1 OR cpf = $1
		`, cpfDigits).Scan(&foundEmail)

		if err == sql.ErrNoRows || !foundEmail.Valid || foundEmail.String == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "CPF ou senha inválidos."})
			return
		} else if err != nil {
			log.Printf("Erro ao buscar e-mail por CPF: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
			return
		}
		email = strings.ToLower(foundEmail.String)
	}

	authResp, status, err := supabaseSignInWithPassword(email, req.Password)
	if err != nil {
		if status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusUnprocessableEntity {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "E-mail/CPF ou senha inválidos."})
			return
		}
		log.Printf("Erro no login Supabase: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao autenticar: " + err.Error()})
		return
	}

	if authResp.User == nil || authResp.AccessToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "E-mail/CPF ou senha inválidos."})
		return
	}

	fullName := ""
	db := config.GetDB()
	var fn sql.NullString
	if err := db.QueryRow(`SELECT full_name FROM users WHERE id = $1`, authResp.User.ID).Scan(&fn); err == nil {
		fullName = fn.String
	}

	c.JSON(http.StatusOK, LoginResponse{
		AccessToken:  authResp.AccessToken,
		RefreshToken: authResp.RefreshToken,
		UserID:       authResp.User.ID,
		Email:        authResp.User.Email,
		FullName:     fullName,
	})
}
