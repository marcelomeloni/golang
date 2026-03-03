package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func OnboardingHandler(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}
	uid := userID.(string)

	var body struct {
		FullName       string `json:"full_name"        binding:"required"`
		CPF            string `json:"cpf"              binding:"required"`
		OrgName        string `json:"org_name"         binding:"required"`
		OrgSlug        string `json:"org_slug"         binding:"required"`
		OrgDescription string `json:"org_description"`
		OrgEmail       string `json:"org_email"`
		OrgPhone       string `json:"org_phone"`
		OrgInstagram   string `json:"org_instagram"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	db := config.GetDB()
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	// 1. Atualiza full_name e cpf do usuário
	_, err = tx.ExecContext(ctx,
		`UPDATE users
		    SET full_name  = $1,
		        cpf        = $2,
		        updated_at = now()
		  WHERE id = $3`,
		body.FullName, body.CPF, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar usuário: " + err.Error()})
		return
	}

	// 2. Criar organização
	orgID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO organizations (id, name, slug, description, email, phone, instagram)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		orgID, body.OrgName, body.OrgSlug, body.OrgDescription,
		body.OrgEmail, body.OrgPhone, body.OrgInstagram,
	)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "slug já em uso ou dados inválidos: " + err.Error()})
		return
	}

	// 3. Vincular como owner
	_, err = tx.ExecContext(ctx,
		`INSERT INTO organization_members (organization_id, user_id, role)
		 VALUES ($1, $2, 'owner')`,
		orgID, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao vincular organização: " + err.Error()})
		return
	}

	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"redirect": "/dashboard",
		"org_id":   orgID,
		"message":  "organização criada com sucesso",
	})
}

// GetOrgHandler — GET /org/:slug
// Retorna os dados da organização. Qualquer membro pode acessar.
func GetOrgHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	// Verifica se o usuário é membro
	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado ou organização não encontrada"})
		return
	}

	type OrgRow struct {
		ID                    string
		Name                  string
		Slug                  string
		Description           *string
		Email                 *string
		Phone                 *string
		Instagram             *string
		Facebook              *string
		Website               *string
		LogoURL               *string
		PlatformFeePercentage float64
		PlatformFeeFixed      float64
		CreatedAt             string
	}

	var o OrgRow
	err = db.QueryRowContext(ctx,
		`SELECT id, name, slug, description, email, phone, instagram,
		        facebook, website, logo_url,
		        platform_fee_percentage, platform_fee_fixed,
		        to_char(created_at, 'YYYY-MM-DD') AS created_at
		   FROM organizations WHERE id = $1`, orgID,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.Description, &o.Email, &o.Phone,
		&o.Instagram, &o.Facebook, &o.Website, &o.LogoURL,
		&o.PlatformFeePercentage, &o.PlatformFeeFixed, &o.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar organização"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id": o.ID, "name": o.Name, "slug": o.Slug,
		"description": o.Description, "email": o.Email, "phone": o.Phone,
		"instagram": o.Instagram, "facebook": o.Facebook, "website": o.Website,
		"logo_url": o.LogoURL,
		"platform_fee_percentage": o.PlatformFeePercentage,
		"platform_fee_fixed":      o.PlatformFeeFixed,
		"created_at":              o.CreatedAt,
	})
}

// UpdateOrgHandler — PATCH /org/:slug
// Atualiza dados da org. Apenas owner ou admin.
func UpdateOrgHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	// Verifica se é owner ou admin
	var role string
	err := db.QueryRowContext(ctx,
		`SELECT om.role FROM organization_members om
		   JOIN organizations o ON o.id = om.organization_id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&role)
	if err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas owner ou admin podem editar a organização"})
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Email       *string `json:"email"`
		Phone       *string `json:"phone"`
		Instagram   *string `json:"instagram"`
		Facebook    *string `json:"facebook"`
		Website     *string `json:"website"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err = db.ExecContext(ctx,
		`UPDATE organizations
		    SET name        = COALESCE($1, name),
		        description = COALESCE($2, description),
		        email       = COALESCE($3, email),
		        phone       = COALESCE($4, phone),
		        instagram   = COALESCE($5, instagram),
		        facebook    = COALESCE($6, facebook),
		        website     = COALESCE($7, website)
		  WHERE slug = $8`,
		body.Name, body.Description, body.Email, body.Phone,
		body.Instagram, body.Facebook, body.Website, slug,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar organização: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "organização atualizada"})
}

func DeleteOrgHandler(c *gin.Context) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	// Apenas owner pode excluir
	var orgID, role string
	err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID, &role)
	if err != nil || role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas o owner pode excluir a organização"})
		return
	}

	// Confirma o nome da org no body para evitar exclusão acidental
	var body struct {
		ConfirmName string `json:"confirm_name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var orgName string
	_ = db.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&orgName)

	if body.ConfirmName != orgName {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nome de confirmação incorreto"})
		return
	}

	// CASCADE no schema já cuida de events, members, tickets etc.
	_, err = db.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao excluir organização: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "organização excluída com sucesso"})
}