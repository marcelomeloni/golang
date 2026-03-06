// controllers/client/auth_controller.go
package client

import (
	"database/sql"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS
// ==========================================

type CheckProfileResponse struct {
	HasProfile bool   `json:"hasProfile"`
	UserID     string `json:"userId"`
	FullName   string `json:"fullName"`
	AvatarURL  string `json:"avatarUrl"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`      // <-- CAMPO ADICIONADO
	CPF        string `json:"cpf"`        // já formatado: 000.000.000-00 (vazio se não tiver)
	BirthDate  string `json:"birthDate"`  // YYYY-MM-DD (vazio se não tiver)
}

type CompleteProfileRequest struct {
	UserID    string `json:"userId"    binding:"required"`
	FullName  string `json:"fullName"  binding:"required"`
	CPF       string `json:"cpf"       binding:"required"`
	BirthDate string `json:"birthDate" binding:"required"` // YYYY-MM-DD
	Phone     string `json:"phone"`
	Username  string `json:"username"`
	Instagram string `json:"instagram"`
}

// ==========================================
// HELPERS
// ==========================================

func cleanCPF(raw string) (string, bool) {
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(raw, "")
	if len(digits) != 11 {
		return "", false
	}
	return digits, true
}

// formatCPF formata 11 dígitos → 000.000.000-00
func formatCPF(digits string) string {
	if len(digits) != 11 {
		return digits
	}
	return digits[0:3] + "." + digits[3:6] + "." + digits[6:9] + "-" + digits[9:11]
}

func cleanPhone(raw string) string {
	re := regexp.MustCompile(`\D`)
	return re.ReplaceAllString(raw, "")
}

// ==========================================
// HANDLERS
// ==========================================

// CheckProfile — retorna dados existentes do usuário incluindo CPF e data de nascimento
// para pré-preencher o formulário de completar registro.
func CheckProfile(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId é obrigatório"})
		return
	}

	db := config.GetDB()

	var (
		cpf       sql.NullString
		birthDate sql.NullTime
		fullName  sql.NullString
		avatarURL sql.NullString
		email     sql.NullString
		phone     sql.NullString // Variável já adicionada
	)

	err := db.QueryRow(`
		SELECT cpf, birth_date, full_name, avatar_url, email, phone
		FROM users
		WHERE id = $1
	`, userID).Scan(&cpf, &birthDate, &fullName, &avatarURL, &email, &phone)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, CheckProfileResponse{HasProfile: false, UserID: userID})
		return
	} else if err != nil {
		log.Printf("Erro ao buscar perfil: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
		return
	}

	// === AQUI ESTÁ A MUDANÇA PRINCIPAL ===
	hasCPF := cpf.Valid && strings.TrimSpace(cpf.String) != ""
	hasBirthDate := birthDate.Valid
	hasPhone := phone.Valid && strings.TrimSpace(phone.String) != "" // Verifica se o telefone existe

	// Agora o perfil só é considerado completo se tiver os 3 campos
	hasProfile := hasCPF && hasBirthDate && hasPhone 
	// =====================================

	// Formata CPF para exibição no frontend (000.000.000-00)
	cpfFormatted := ""
	if hasCPF {
		cpfFormatted = formatCPF(strings.TrimSpace(cpf.String))
	}

	// Formata data para YYYY-MM-DD
	birthDateStr := ""
	if hasBirthDate {
		birthDateStr = birthDate.Time.Format("2006-01-02")
	}

	c.JSON(http.StatusOK, CheckProfileResponse{
		HasProfile: hasProfile,
		UserID:     userID,
		FullName:   fullName.String,
		AvatarURL:  avatarURL.String,
		Email:      email.String,
		Phone:      phone.String, 
		CPF:        cpfFormatted,
		BirthDate:  birthDateStr,
	})
}

// CompleteProfile — salva ou atualiza CPF, data de nascimento e dados extras.
func CompleteProfile(c *gin.Context) {
	var req CompleteProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos: " + err.Error()})
		return
	}

	cpfClean, ok := cleanCPF(req.CPF)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CPF inválido. Informe 11 dígitos."})
		return
	}

	birthDate, err := time.Parse("2006-01-02", req.BirthDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Data de nascimento inválida. Use o formato YYYY-MM-DD."})
		return
	}

	minAge := time.Now().AddDate(-16, 0, 0)
	if birthDate.After(minAge) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "É necessário ter pelo menos 16 anos para se cadastrar."})
		return
	}

	phoneClean := cleanPhone(req.Phone)
	username   := strings.TrimSpace(strings.TrimPrefix(req.Username, "@"))
	instagram  := strings.TrimSpace(strings.TrimPrefix(req.Instagram, "@"))

	db := config.GetDB()

	// CPF duplicado em outra conta?
	var existingID string
	err = db.QueryRow(
		`SELECT id FROM users WHERE cpf = $1 AND id != $2`,
		cpfClean, req.UserID,
	).Scan(&existingID)
	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Este CPF já está cadastrado em outra conta."})
		return
	}

	// Username duplicado?
	if username != "" {
		err = db.QueryRow(
			`SELECT id FROM users WHERE username = $1 AND id != $2`,
			username, req.UserID,
		).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "Este nome de usuário já está em uso."})
			return
		}
	}

	_, err = db.Exec(`
		UPDATE users
		SET
			full_name   = $1,
			cpf         = $2,
			birth_date  = $3,
			phone       = $4,
			username    = NULLIF($5, ''),
			instagram   = NULLIF($6, ''),
			updated_at  = $7
		WHERE id = $8
	`,
		strings.TrimSpace(req.FullName),
		cpfClean,
		birthDate,
		phoneClean,
		username,
		instagram,
		time.Now(),
		req.UserID,
	)
	if err != nil {
		log.Printf("Erro ao completar perfil: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar perfil"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}