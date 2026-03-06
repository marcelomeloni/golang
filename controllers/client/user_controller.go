// controllers/client/user_controller.go
package client

import (
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/storage"
	"github.com/gin-gonic/gin"
)

// ==========================================
// ESTRUTURAS
// ==========================================

type UserProfileResponse struct {
	ID                  string `json:"id"`
	FullName            string `json:"fullName"`
	Email               string `json:"email"`
	CPF                 string `json:"cpf"`
	Phone               string `json:"phone"`
	Instagram           string `json:"instagram"`
	AvatarURL           string `json:"avatarUrl"`
	BirthDate           string `json:"birthDate"` // YYYY-MM-DD
	AttendedEventsCount int    `json:"attendedEventsCount"`
}

type UpdateProfileRequest struct {
	FullName  string `json:"fullName"`
	Phone     string `json:"phone"`
	Instagram string `json:"instagram"`
}

// ==========================================
// HANDLERS
// ==========================================

// GetUserProfile — retorna o perfil completo do usuário autenticado
func GetUserProfile(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId é obrigatório"})
		return
	}

	db := config.GetDB()

	var (
		id                  string
		fullName            sql.NullString
		email               sql.NullString
		cpf                 sql.NullString
		phone               sql.NullString
		instagram           sql.NullString
		avatarURL           sql.NullString
		birthDate           sql.NullTime
		attendedEventsCount int
	)

	err := db.QueryRow(`
		SELECT
			id,
			COALESCE(full_name, ''),
			COALESCE(email, ''),
			COALESCE(cpf, ''),
			COALESCE(phone, ''),
			COALESCE(instagram, ''),
			COALESCE(avatar_url, ''),
			birth_date,
			COALESCE(attended_events_count, 0)
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&id, &fullName, &email, &cpf, &phone,
		&instagram, &avatarURL, &birthDate, &attendedEventsCount,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Usuário não encontrado"})
		return
	} else if err != nil {
		log.Printf("Erro ao buscar perfil: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro interno"})
		return
	}

	cpfFormatted := ""
	if cpf.String != "" {
		cpfFormatted = formatCPF(strings.TrimSpace(cpf.String))
	}

	birthDateStr := ""
	if birthDate.Valid {
		birthDateStr = birthDate.Time.Format("2006-01-02")
	}

	c.JSON(http.StatusOK, UserProfileResponse{
		ID:                  id,
		FullName:            fullName.String,
		Email:               email.String,
		CPF:                 cpfFormatted,
		Phone:               phone.String,
		Instagram:           instagram.String,
		AvatarURL:           avatarURL.String,
		BirthDate:           birthDateStr,
		AttendedEventsCount: attendedEventsCount,
	})
}

// UpdateUserProfile — atualiza campos editáveis.
// Regra de negócio: CPF e data de nascimento são imutáveis após o onboarding.
func UpdateUserProfile(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId é obrigatório"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Dados inválidos"})
		return
	}

	phone := cleanPhone(req.Phone)
	instagram := strings.TrimSpace(strings.TrimPrefix(req.Instagram, "@"))
	fullName := strings.TrimSpace(req.FullName)

	db := config.GetDB()

	_, err := db.Exec(`
		UPDATE users
		SET
			full_name  = NULLIF($1, ''),
			phone      = NULLIF($2, ''),
			instagram  = NULLIF($3, ''),
			updated_at = $4
		WHERE id = $5
	`, fullName, phone, instagram, time.Now(), userID)

	if err != nil {
		log.Printf("Erro ao atualizar perfil: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar alterações"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UploadUserAvatar — processa a imagem do usuário e atualiza a URL no perfil
func UploadUserAvatar(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId é obrigatório"})
		return
	}

	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nenhuma imagem enviada."})
		return
	}
	defer file.Close()

	// Envia para o bucket configurado na env, usando a pasta profilepic
	uploadResult, err := storage.UploadOrgImage(file, header, "profilepic")
	if err != nil {
		log.Printf("Erro no upload do avatar: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao processar imagem"})
		return
	}

	db := config.GetDB()
	_, err = db.Exec(`
		UPDATE users
		SET avatar_url = $1, updated_at = $2
		WHERE id = $3
	`, uploadResult.URL, time.Now(), userID)

	if err != nil {
		log.Printf("Erro ao salvar URL no banco: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar o perfil"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"avatarUrl": uploadResult.URL,
	})
}