package admin

import (
	"database/sql"
	"log"
	"net/http"
	"strings"

	"bilheteria-api/config"
	"bilheteria-api/services/supabaseauth"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS
// ==========================================

type LoginRequest struct {
	Email    string `json:"email"    binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       string `json:"userId"`
	Email        string `json:"email"`
	FullName     string `json:"fullName"`
	Role         string `json:"role"`
}

type Profile struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	FullName string `json:"fullName"`
	Role     string `json:"role"`
}

// ==========================================
// HELPERS
// ==========================================

// platformAdminRole devolve o papel do usuário em platform_admins.
// O segundo retorno indica se ele é admin da plataforma.
func platformAdminRole(db *sql.DB, userID string) (string, bool) {
	var role string
	err := db.QueryRow(
		`SELECT role FROM platform_admins WHERE user_id = $1`, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		log.Printf("platformAdminRole: %v", err)
		return "", false
	}
	return role, true
}

// loadProfile monta o perfil do admin a partir de users + platform_admins.
func loadProfile(db *sql.DB, userID string) (Profile, bool) {
	role, ok := platformAdminRole(db, userID)
	if !ok {
		return Profile{}, false
	}

	var email, fullName sql.NullString
	_ = db.QueryRow(`SELECT email, full_name FROM users WHERE id = $1`, userID).
		Scan(&email, &fullName)

	return Profile{
		UserID:   userID,
		Email:    email.String,
		FullName: fullName.String,
		Role:     role,
	}, true
}

// ==========================================
// HANDLERS
// ==========================================

// AdminLogin autentica um administrador da plataforma.
//
// Além de validar a senha no Supabase Auth, exige que o usuário exista em
// platform_admins — caso contrário um usuário comum conseguiria entrar no
// painel só por ter credenciais válidas.
func AdminLogin(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))

	session, status, err := supabaseauth.SignInWithPassword(email, req.Password)
	if err != nil {
		if status == http.StatusBadRequest || status == http.StatusUnauthorized {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "E-mail ou senha inválidos."})
			return
		}
		log.Printf("AdminLogin supabase: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "Erro ao autenticar."})
		return
	}

	db := config.GetDB()
	profile, ok := loadProfile(db, session.UserID)
	if !ok {
		// Credenciais válidas, mas não é admin: mesma resposta para não
		// revelar quais contas existem.
		c.JSON(http.StatusForbidden, gin.H{"error": "Acesso restrito a administradores da plataforma."})
		return
	}
	profile.Email = session.Email

	c.JSON(http.StatusOK, LoginResponse{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		UserID:       profile.UserID,
		Email:        profile.Email,
		FullName:     profile.FullName,
		Role:         profile.Role,
	})
}

// AdminMe devolve o perfil do admin autenticado (rota protegida).
func AdminMe(c *gin.Context) {
	userID, _ := c.Get("userID")
	uid, _ := userID.(string)

	db := config.GetDB()
	profile, ok := loadProfile(db, uid)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso restrito"})
		return
	}

	c.JSON(http.StatusOK, profile)
}
