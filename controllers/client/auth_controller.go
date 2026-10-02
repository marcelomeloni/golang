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
	"time"

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

// supabaseServiceRoleKey — chave administrativa do Supabase.
// ATENÇÃO: nunca expor no frontend. Usada apenas no servidor para
// reconciliar contas já existentes durante o claim de guest.
func supabaseServiceRoleKey() string {
	return os.Getenv("SUPABASE_SERVICE_KEY")
}

// supabaseAdminFindUserByEmail procura um usuário já existente no Supabase Auth.
// O endpoint de listagem não aceita filtro por e-mail, então pagina até achar.
func supabaseAdminFindUserByEmail(email string) (*supabaseUser, error) {
	key := supabaseServiceRoleKey()
	if key == "" {
		return nil, fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY não configurada")
	}

	target := strings.ToLower(strings.TrimSpace(email))

	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("%s/auth/v1/admin/users?page=%d&per_page=1000", supabaseURL(), page)

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("apikey", key)
		req.Header.Set("Authorization", "Bearer "+key)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("admin listUsers: %s", supabaseErrMessage(body))
		}

		var pageResp struct {
			Users []supabaseUser `json:"users"`
		}
		if err := json.Unmarshal(body, &pageResp); err != nil {
			return nil, err
		}

		for i := range pageResp.Users {
			if strings.EqualFold(pageResp.Users[i].Email, target) {
				return &pageResp.Users[i], nil
			}
		}

		if len(pageResp.Users) < 1000 {
			break
		}
	}

	return nil, nil
}

// supabaseAdminSetPassword redefine a senha de uma conta existente e confirma
// o e-mail. Usado no claim quando o e-mail já está cadastrado no Supabase.
func supabaseAdminSetPassword(userID, password string) error {
	key := supabaseServiceRoleKey()
	if key == "" {
		return fmt.Errorf("SUPABASE_SERVICE_ROLE_KEY não configurada")
	}

	b, _ := json.Marshal(map[string]interface{}{
		"password":      password,
		"email_confirm": true,
	})

	req, err := http.NewRequest(http.MethodPut, supabaseURL()+"/auth/v1/admin/users/"+userID, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", key)
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("admin update user: %s", supabaseErrMessage(body))
	}

	return nil
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

// ==========================================
// CLAIM GUEST — converte conta guest em conta real
// ==========================================

type CheckGuestRequest struct {
	Email string `json:"email" binding:"required,email"`
	CPF   string `json:"cpf"   binding:"required"`
}

// Resposta pública e mínima: sem CPF, sem e-mail e sem id.
type CheckGuestResponse struct {
	IsGuest    bool   `json:"isGuest"`
	FullName   string `json:"fullName"`
	HasTickets bool   `json:"hasTickets"`
}

// guestNotFound responde sempre a mesma coisa, para não servir de oráculo
// de enumeração: quem chamar não consegue distinguir "não existe" de
// "existe, mas não é guest" ou "CPF não bate".
func guestNotFound(c *gin.Context) {
	c.JSON(http.StatusOK, CheckGuestResponse{IsGuest: false})
}

// CheckGuest — POST (não GET) para não vazar CPF/e-mail nos logs de acesso.
// Exige e-mail E CPF do mesmo guest.
func CheckGuest(c *gin.Context) {
	var req CheckGuestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		guestNotFound(c)
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	cpfDigits, ok := cleanCPF(req.CPF)
	if email == "" || !ok {
		guestNotFound(c)
		return
	}

	db := config.GetDB()

	var userID, fullName string
	err := db.QueryRow(`
		SELECT id, COALESCE(full_name, '')
		FROM users
		WHERE lower(email) = $1 AND cpf_digits = $2 AND is_guest = true
	`, email, cpfDigits).Scan(&userID, &fullName)

	if err == sql.ErrNoRows {
		guestNotFound(c)
		return
	} else if err != nil {
		log.Printf("CheckGuest query error: %v", err)
		guestNotFound(c)
		return
	}

	hasTickets := false
	if err := db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM tickets WHERE user_id = $1 AND status = 'valid')
	`, userID).Scan(&hasTickets); err != nil {
		log.Printf("CheckGuest tickets error: %v", err)
	}

	c.JSON(http.StatusOK, CheckGuestResponse{
		IsGuest:    true,
		FullName:   fullName,
		HasTickets: hasTickets,
	})
}

type ClaimGuestRequest struct {
	Email     string `json:"email"     binding:"required,email"`
	CPF       string `json:"cpf"       binding:"required"`
	Password  string `json:"password"  binding:"required,min=6"`
	FullName  string `json:"fullName"`
	BirthDate string `json:"birthDate"` // YYYY-MM-DD, opcional
	Phone     string `json:"phone"`     // opcional
}

// ClaimGuest — converte a conta guest em conta real.
// Exige e-mail E CPF do mesmo guest (prova de posse), preserva o perfil
// existente e transfere ingressos/pedidos/transações numa única transação.
func ClaimGuest(c *gin.Context) {
	var req ClaimGuestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	cpfDigits, ok := cleanCPF(req.CPF)
	if email == "" || !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Informe e-mail e CPF válidos."})
		return
	}

	db := config.GetDB()

	// 1. Guest só é encontrado quando e-mail e CPF pertencem à mesma linha.
	var (
		guestID, guestName, guestCPF                   string
		guestPhone, guestAvatar                        sql.NullString
		guestUsername, guestInstagram, guestPixKey     sql.NullString
		guestPixKeyType                                sql.NullString
		guestBirth                                     sql.NullTime
	)

	err := db.QueryRow(`
		SELECT id,
		       COALESCE(full_name, ''), COALESCE(cpf, ''),
		       phone, avatar_url, username, instagram, birth_date, pix_key, pix_key_type
		FROM users
		WHERE lower(email) = $1 AND cpf_digits = $2 AND is_guest = true
	`, email, cpfDigits).Scan(
		&guestID, &guestName, &guestCPF,
		&guestPhone, &guestAvatar, &guestUsername, &guestInstagram,
		&guestBirth, &guestPixKey, &guestPixKeyType,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Não encontramos nenhuma compra com este e-mail e CPF."})
		return
	} else if err != nil {
		log.Printf("ClaimGuest find guest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
		return
	}

	fullName := strings.TrimSpace(req.FullName)
	if fullName == "" {
		fullName = guestName
	}

	// O que a pessoa digitou no formulário completa o que faltava no guest.
	// O que já existir na conta real tem prioridade (garantido pelo COALESCE
	// do ON CONFLICT mais abaixo).
	birthDate := guestBirth
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(req.BirthDate)); err == nil {
		if t.After(time.Now().AddDate(-16, 0, 0)) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "É necessário ter pelo menos 16 anos para se cadastrar.",
				"code":  "age_restriction",
			})
			return
		}
		birthDate = sql.NullTime{Time: t, Valid: true}
	}

	phone := guestPhone
	if cleaned := cleanPhone(req.Phone); cleaned != "" {
		phone = sql.NullString{String: cleaned, Valid: true}
	}

	// 2. Garante um usuário no Supabase Auth com esse e-mail.
	authResp, status, signErr := supabaseSignUp(email, req.Password, fullName)

	switch {
	case signErr == nil:
		if authResp.User == nil || authResp.User.ID == "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Resposta inesperada do serviço de autenticação"})
			return
		}
		// Se o projeto exige confirmação de e-mail o signup não volta com
		// tokens, então autentica para obter a sessão.
		if authResp.AccessToken == "" {
			if loginResp, _, lerr := supabaseSignInWithPassword(email, req.Password); lerr == nil {
				authResp = loginResp
			}
		}

	case status == http.StatusUnprocessableEntity ||
		status == http.StatusBadRequest ||
		status == http.StatusConflict:
		// 3. Já existe conta no Supabase (cadastro anterior ou login Google).
		// O usuário provou posse com e-mail + CPF, então a senha escolhida
		// aqui passa a valer para essa conta.
		adminUser, ferr := supabaseAdminFindUserByEmail(email)
		if ferr != nil {
			log.Printf("ClaimGuest admin lookup error: %v", ferr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
			return
		}
		if adminUser == nil || adminUser.ID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "E-mail ou senha inválidos."})
			return
		}

		if err := supabaseAdminSetPassword(adminUser.ID, req.Password); err != nil {
			log.Printf("ClaimGuest admin set password error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao definir a senha"})
			return
		}

		loginResp, _, lerr := supabaseSignInWithPassword(email, req.Password)
		if lerr != nil || loginResp.User == nil {
			log.Printf("ClaimGuest signin after claim error: %v", lerr)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "E-mail ou senha inválidos."})
			return
		}
		authResp = loginResp

	default:
		log.Printf("ClaimGuest supabase signup error: %v", signErr)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao criar conta"})
		return
	}

	if authResp == nil || authResp.User == nil || authResp.User.ID == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Resposta inesperada do serviço de autenticação"})
		return
	}

	newUserID := authResp.User.ID

	// 4. Transfere os dados do guest para a conta real, em uma transação.
	tx, err := db.Begin()
	if err != nil {
		log.Printf("ClaimGuest tx begin error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
		return
	}
	defer tx.Rollback()

	transfers := []struct {
		label string
		query string
	}{
		{"ingressos", `UPDATE tickets SET user_id = $1 WHERE user_id = $2`},
		{"transações do market", `UPDATE market_transactions SET buyer_id = $1 WHERE buyer_id = $2`},
		{"pedidos", `UPDATE orders SET user_id = $1 WHERE user_id = $2`},
	}

	for _, t := range transfers {
		if _, err := tx.Exec(t.query, newUserID, guestID); err != nil {
			log.Printf("ClaimGuest transfer %s error: %v", t.label, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao transferir seus dados"})
			return
		}
	}

	// Preserva o que já existir na conta real e completa o que faltar com o
	// perfil do guest (nada de DELETE + INSERT, que perderia os campos).
	_, err = tx.Exec(`
		INSERT INTO users (
			id, email, full_name, cpf, phone, avatar_url,
			username, instagram, birth_date, pix_key, pix_key_type, is_guest
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, false)
		ON CONFLICT (id) DO UPDATE SET
			email        = EXCLUDED.email,
			full_name    = COALESCE(NULLIF(users.full_name, ''), EXCLUDED.full_name),
			cpf          = COALESCE(NULLIF(users.cpf, ''), EXCLUDED.cpf),
			phone        = COALESCE(NULLIF(users.phone, ''), EXCLUDED.phone),
			avatar_url   = COALESCE(NULLIF(users.avatar_url, ''), EXCLUDED.avatar_url),
			username     = COALESCE(NULLIF(users.username, ''), EXCLUDED.username),
			instagram    = COALESCE(NULLIF(users.instagram, ''), EXCLUDED.instagram),
			birth_date   = COALESCE(users.birth_date, EXCLUDED.birth_date),
			pix_key      = COALESCE(NULLIF(users.pix_key, ''), EXCLUDED.pix_key),
			pix_key_type = COALESCE(NULLIF(users.pix_key_type, ''), EXCLUDED.pix_key_type),
			is_guest     = false,
			updated_at   = NOW()
	`,
		newUserID, email, fullName, guestCPF, phone, guestAvatar,
		guestUsername, guestInstagram, birthDate, guestPixKey, guestPixKeyType,
	)
	if err != nil {
		log.Printf("ClaimGuest upsert user error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao criar conta real"})
		return
	}

	// O guest só é removido depois que nenhuma referência aponta para ele.
	if _, err := tx.Exec(`DELETE FROM users WHERE id = $1`, guestID); err != nil {
		log.Printf("ClaimGuest delete guest error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao remover conta guest"})
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("ClaimGuest commit error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao finalizar"})
		return
	}

	if authResp.AccessToken == "" || authResp.RefreshToken == "" {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "Conta convertida, mas não foi possível iniciar a sessão. Faça login.",
		})
		return
	}

	// 5. Retorna tokens
	c.JSON(http.StatusOK, LoginResponse{
		AccessToken:  authResp.AccessToken,
		RefreshToken: authResp.RefreshToken,
		UserID:       newUserID,
		Email:        email,
		FullName:     fullName,
	})
}
