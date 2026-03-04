package organizer

import (
	"context"
	"net/http"
	"regexp"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

func GetMembersHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID)
	
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	rows, err := db.QueryContext(ctx,
		`SELECT om.id, om.user_id, om.role,
				to_char(om.created_at, 'YYYY-MM-DD') AS joined_at,
				u.full_name, u.email, u.cpf, u.avatar_url
		   FROM organization_members om
		   JOIN users u ON u.id = om.user_id
		  WHERE om.organization_id = $1
		  ORDER BY om.created_at ASC`, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar membros"})
		return
	}
	defer rows.Close()

	type MemberRow struct {
		ID        string
		UserID    string
		Role      string
		JoinedAt  string
		FullName  *string
		Email     *string
		CPF       *string
		AvatarURL *string
	}

	var members []gin.H
	for rows.Next() {
		var m MemberRow
		if err := rows.Scan(&m.ID, &m.UserID, &m.Role, &m.JoinedAt,
			&m.FullName, &m.Email, &m.CPF, &m.AvatarURL); err != nil {
			continue
		}
		members = append(members, gin.H{
			"id":         m.ID,
			"user_id":    m.UserID,
			"role":       m.Role,
			"joined_at":  m.JoinedAt,
			"full_name":  m.FullName,
			"email":      m.Email,
			"cpf":        m.CPF,
			"avatar_url": m.AvatarURL,
		})
	}

	if members == nil {
		members = []gin.H{}
	}

	c.JSON(http.StatusOK, members)
}

func AddMemberHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID, role string
	err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID, &role)
	
	if err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente"})
		return
	}

	var body struct {
		CPF  string `json:"cpf"  binding:"required"`
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validRoles := map[string]bool{"admin": true, "promoter": true, "checkin_staff": true}
	if !validRoles[body.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role inválida"})
		return
	}

	// Normaliza o CPF limpando pontuações da string recebida no JSON
	re := regexp.MustCompile(`\D`)
	cleanCPF := re.ReplaceAllString(body.CPF, "")

	var targetUserID string
	// Normaliza o dado da tabela users via regex_replace do Postgres no momento da consulta
	err = db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE regexp_replace(cpf, '\D', '', 'g') = $1`, cleanCPF,
	).Scan(&targetUserID)
	
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO organization_members (organization_id, user_id, role)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (organization_id, user_id) DO UPDATE SET role = $3`,
		orgID, targetUserID, body.Role,
	)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na inserção"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "membro adicionado"})
}

func UpdateMemberRoleHandler(c *gin.Context) {
	slug := c.Param("slug")
	memberID := c.Param("memberID")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID, role string
	err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID, &role)
	
	if err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente"})
		return
	}

	var body struct {
		Role string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	validRoles := map[string]bool{"admin": true, "promoter": true, "checkin_staff": true}
	if !validRoles[body.Role] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role inválida"})
		return
	}

	_, err = db.ExecContext(ctx,
		`UPDATE organization_members SET role = $1
		  WHERE id = $2 AND organization_id = $3`,
		body.Role, memberID, orgID,
	)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na atualização"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "role atualizada"})
}

func RemoveMemberHandler(c *gin.Context) {
	slug := c.Param("slug")
	memberID := c.Param("memberID")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	var orgID, role string
	err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID, &role)
	
	if err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente"})
		return
	}

	var targetRole string
	_ = db.QueryRowContext(ctx,
		`SELECT role FROM organization_members WHERE id = $1`, memberID,
	).Scan(&targetRole)
	
	if targetRole == "owner" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operação não permitida"})
		return
	}

	_, err = db.ExecContext(ctx,
		`DELETE FROM organization_members WHERE id = $1 AND organization_id = $2`,
		memberID, orgID,
	)
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro na exclusão"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "membro removido"})
}