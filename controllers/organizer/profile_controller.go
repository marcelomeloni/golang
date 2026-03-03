package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

func GetProfileHandler(c *gin.Context) {
	userID, _ := c.Get("userID")
	uid := userID.(string)
	db := config.GetDB()

	type Row struct {
		ID        string
		FullName  *string
		Email     *string
		CPF       *string
		AvatarURL *string
	}
	var r Row
	err := db.QueryRowContext(context.Background(),
		`SELECT id, full_name, email, cpf, avatar_url FROM users WHERE id = $1`, uid,
	).Scan(&r.ID, &r.FullName, &r.Email, &r.CPF, &r.AvatarURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         r.ID,
		"full_name":  r.FullName,
		"email":      r.Email,
		"cpf":        r.CPF,
		"avatar_url": r.AvatarURL,
	})
}

func UpdateProfileHandler(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}
	uid := userID.(string)

	var body struct {
		CPF string `json:"cpf" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := config.GetDB().ExecContext(context.Background(),
		`UPDATE users SET cpf = $1, updated_at = now() WHERE id = $2`,
		body.CPF, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar perfil: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "perfil atualizado"})
}