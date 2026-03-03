package organizer

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"bilheteria-api/config"
)

func AuthCallbackHandler(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}

	uid, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "userID inválido no contexto"})
		return
	}

	db := config.GetDB()
	ctx := context.Background()

	// ── 1. Buscar dados do usuário ────────────────────────────────────────────
	type UserRow struct {
		ID        string
		Email     *string
		FullName  *string
		CPF       *string
		Phone     *string
		AvatarURL *string
		IsGuest   bool
	}

	var user UserRow
	err := db.QueryRowContext(ctx,
		`SELECT id, email, full_name, cpf, phone, avatar_url, is_guest
		   FROM users
		  WHERE id = $1`, uid,
	).Scan(&user.ID, &user.Email, &user.FullName, &user.CPF,
		&user.Phone, &user.AvatarURL, &user.IsGuest)

	if err != nil {
		// Usuário ainda não existe em public.users — cria espelho
		_, err2 := db.ExecContext(ctx,
			`INSERT INTO users (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, uid,
		)
		if err2 != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar usuário"})
			return
		}
		user.ID = uid
	}

	// ── 2. Checar se já tem organização ───────────────────────────────────────
	var orgID string
	_ = db.QueryRowContext(ctx,
		`SELECT organization_id
		   FROM organization_members
		  WHERE user_id = $1
		  LIMIT 1`, uid,
	).Scan(&orgID)

	// ── 3. Montar payload ─────────────────────────────────────────────────────
	userPayload := gin.H{
		"id":         user.ID,
		"email":      user.Email,
		"full_name":  user.FullName,
		"cpf":        user.CPF,
		"phone":      user.Phone,
		"avatar_url": user.AvatarURL,
		"is_guest":   user.IsGuest,
	}

	if orgID != "" {
		c.JSON(http.StatusOK, gin.H{
			"redirect": "/dashboard",
			"user":     userPayload,
			"org_id":   orgID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"redirect": "/onboarding",
		"user":     userPayload,
	})
}

