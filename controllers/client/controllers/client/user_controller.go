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

type UserProfileResponse struct {
	ID                  string `json:"id"`
	FullName            string `json:"fullName"`
	Email               string `json:"email"`
	CPF                 string `json:"cpf"`
	Phone               string `json:"phone"`
	Instagram           string `json:"instagram"`
	AvatarURL           string `json:"avatarUrl"`
	BirthDate           string `json:"birthDate"`
	AttendedEventsCount int    `json:"attendedEventsCount"`
	PixKey              string `json:"pixKey"`
	PixKeyType          string `json:"pixKeyType"`
}

type UpdateProfileRequest struct {
	FullName  string `json:"fullName"`
	Phone     string `json:"phone"`
	Instagram string `json:"instagram"`
}

type UpdatePixKeyRequest struct {
	PixKey     string `json:"pixKey"     binding:"required"`
	PixKeyType string `json:"pixKeyType" binding:"required"`
}

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
		pixKey              sql.NullString
		pixKeyType          sql.NullString
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
			COALESCE(attended_events_count, 0),
			pix_key,
			pix_key_type
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&id, &fullName, &email, &cpf, &phone,
		&instagram, &avatarURL, &birthDate, &attendedEventsCount,
		&pixKey, &pixKeyType,
	)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Usuário não encontrado"})
		return
	} else if err != nil {
		log.Printf("GetUserProfile: %v", err)
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
		PixKey:              pixKey.String,
		PixKeyType:          pixKeyType.String,
	})
}

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

	phone     := cleanPhone(req.Phone)
	instagram := strings.TrimSpace(strings.TrimPrefix(req.Instagram, "@"))
	fullName  := strings.TrimSpace(req.FullName)

	db := config.GetDB()

	if phone != "" {
		var existingID string
		err := db.QueryRow(
			`SELECT id FROM users WHERE phone = $1 AND id != $2`,
			phone, userID,
		).Scan(&existingID)
		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Este telefone já está cadastrado em outra conta.",
				"code":  "phone_conflict",
			})
			return
		}
	}

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
		log.Printf("UpdateUserProfile: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar alterações"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// UpdatePixKey salva ou atualiza a chave PIX do usuário para recebimento de vendas no Reppy Market.
func UpdatePixKey(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "userId é obrigatório"})
		return
	}

	var req UpdatePixKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pixKey e pixKeyType são obrigatórios"})
		return
	}

	validTypes := map[string]bool{"cpf": true, "email": true, "phone": true, "random": true}
	if !validTypes[req.PixKeyType] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pixKeyType inválido"})
		return
	}

	_, err := config.GetDB().Exec(`
		UPDATE users SET pix_key = $1, pix_key_type = $2, updated_at = $3 WHERE id = $4
	`, strings.TrimSpace(req.PixKey), req.PixKeyType, time.Now(), userID)
	if err != nil {
		log.Printf("UpdatePixKey: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao salvar chave PIX"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

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

	uploadResult, err := storage.UploadOrgImage(file, header, "profilepic")
	if err != nil {
		log.Printf("UploadUserAvatar upload: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao processar imagem"})
		return
	}

	_, err = config.GetDB().Exec(`
		UPDATE users SET avatar_url = $1, updated_at = $2 WHERE id = $3
	`, uploadResult.URL, time.Now(), userID)
	if err != nil {
		log.Printf("UploadUserAvatar save: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao atualizar o perfil"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "avatarUrl": uploadResult.URL})
}