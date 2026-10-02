package organizer

import (
	"context"
	"database/sql"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/storage"
	"github.com/gin-gonic/gin"
)

func UploadOrgLogoHandler(c *gin.Context) {
	uploadOrgImage(c, "org-logos", "logo_url")
}

// UploadOrgBannerHandler — POST /org/:slug/banner
// Recebe multipart/form-data com campo "file" e atualiza banner_url da org.
func UploadOrgBannerHandler(c *gin.Context) {
	uploadOrgImage(c, "org-banners", "banner_url")
}

// uploadOrgImage é a lógica compartilhada entre logo e banner.
func uploadOrgImage(c *gin.Context, storageFolder, dbColumn string) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := context.Background()

	if !hasEditPermission(ctx, db, slug, uid) {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas owner ou admin podem alterar imagens"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "arquivo não encontrado no campo 'file'"})
		return
	}
	defer file.Close()

	result, err := storage.UploadOrgImage(file, header, storageFolder)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	// dbColumn é controlado internamente ("logo_url" ou "banner_url") — sem risco de SQL injection.
	query := "UPDATE organizations SET " + dbColumn + " = $1, updated_at = now() WHERE slug = $2"
	if _, err = db.ExecContext(ctx, query, result.URL, slug); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao salvar URL da imagem"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": result.URL})
}

// hasEditPermission verifica se o usuário tem role owner ou admin na organização.
func hasEditPermission(ctx context.Context, db *sql.DB, slug, uid string) bool {
	var role string
	err := db.QueryRowContext(ctx,
		`SELECT om.role
		   FROM organization_members om
		   JOIN organizations o ON o.id = om.organization_id
		  WHERE o.slug = $1 AND om.user_id = $2`,
		slug, uid,
	).Scan(&role)
	return err == nil && (role == "owner" || role == "admin")
}