package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

func DashboardHandler(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	// ── 1. Dados do usuário ───────────────────────────────────────────────────
	type UserRow struct {
		ID          string
		FullName    *string
		Email       *string
		CPF         *string
		AvatarURL   *string
		MemberSince string
	}
	var user UserRow
	err := db.QueryRowContext(ctx,
		`SELECT id, full_name, email, cpf, avatar_url,
		        to_char(created_at, 'YYYY') AS member_since
		   FROM users WHERE id = $1`, uid,
	).Scan(&user.ID, &user.FullName, &user.Email, &user.CPF,
		&user.AvatarURL, &user.MemberSince)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	// ── 2. Orgs + contagem de eventos (1 query só) ────────────────────────────
	rows, err := db.QueryContext(ctx,
		`SELECT o.id, o.name, o.slug, o.logo_url, om.role,
		        COUNT(e.id) AS events_count
		   FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		   LEFT JOIN events e ON e.organization_id = o.id
		  WHERE om.user_id = $1
		  GROUP BY o.id, o.name, o.slug, o.logo_url, om.role
		  ORDER BY o.name`, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar organizações"})
		return
	}
	defer rows.Close()

	type OrgRow struct {
		ID          string
		Name        string
		Slug        string
		LogoURL     *string
		Role        string
		EventsCount int
	}

	var orgs []gin.H
	for rows.Next() {
		var o OrgRow
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.LogoURL, &o.Role, &o.EventsCount); err != nil {
			continue
		}
		orgs = append(orgs, gin.H{
			"id":           o.ID,
			"name":         o.Name,
			"slug":         o.Slug,
			"logo_url":     o.LogoURL,
			"role":         o.Role,
			"events_count": o.EventsCount,
		})
	}

	if orgs == nil {
		orgs = []gin.H{}
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":           user.ID,
			"full_name":    user.FullName,
			"email":        user.Email,
			"cpf":          user.CPF,
			"avatar_url":   user.AvatarURL,
			"member_since": user.MemberSince,
		},
		"orgs": orgs,
	})
}