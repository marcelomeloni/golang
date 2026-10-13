package organizer

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"bilheteria-api/config"
	"bilheteria-api/services/payoutservice"
	"github.com/gin-gonic/gin"
)

// resolveOrgMember resolve o id da organização pelo slug e valida que o usuário
// autenticado é owner ou admin dela.
func resolveOrgMember(c *gin.Context) (orgID string, ok bool) {
	slug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid, _ := userID.(string)
	if uid == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return "", false
	}

	var role string
	err := config.GetDB().QueryRow(
		`SELECT o.id, om.role FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`, slug, uid,
	).Scan(&orgID, &role)
	if err != nil || (role != "owner" && role != "admin") {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return "", false
	}
	return orgID, true
}

func ptr(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

func ptrTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// nullIfEmpty converte um NullString em um valor compatível com NULL do banco.
func nullIfEmpty(s sql.NullString) interface{} {
	if !s.Valid || s.String == "" {
		return nil
	}
	return s.String
}

// PayoutRow é um pedido de repasse visto pela organização.
type PayoutRow struct {
	ID         string     `json:"id"`
	Amount     float64    `json:"amount"`
	Status     string     `json:"status"`
	PixKey     *string    `json:"pix_key"`
	PixKeyType *string    `json:"pix_key_type"`
	HolderName *string    `json:"holder_name"`
	Note       *string    `json:"note"`
	AdminNote  *string    `json:"admin_note"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at"`
	PaidAt     *time.Time `json:"paid_at"`
}

// GetOrgBalanceHandler — GET /org/:slug/balance
func GetOrgBalanceHandler(c *gin.Context) {
	orgID, ok := resolveOrgMember(c)
	if !ok {
		return
	}

	balance, err := payoutservice.OrgBalance(config.GetDB(), orgID)
	if err != nil {
		log.Printf("GetOrgBalanceHandler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao calcular saldo"})
		return
	}

	c.JSON(http.StatusOK, balance)
}

// GetPayoutRequestsHandler — GET /org/:slug/payout-requests
func GetPayoutRequestsHandler(c *gin.Context) {
	orgID, ok := resolveOrgMember(c)
	if !ok {
		return
	}

	rows, err := config.GetDB().Query(`
		SELECT id, amount, status, pix_key, pix_key_type, holder_name,
		       note, admin_note, created_at, reviewed_at, paid_at
		FROM payout_requests
		WHERE organization_id = $1
		ORDER BY created_at DESC`, orgID,
	)
	if err != nil {
		log.Printf("GetPayoutRequestsHandler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar repasses"})
		return
	}
	defer rows.Close()

	list := []PayoutRow{}
	for rows.Next() {
		var (
			r                          PayoutRow
			pixKey, pixKeyType         sql.NullString
			holderName, note, adminNote sql.NullString
			reviewedAt, paidAt         sql.NullTime
		)
		if err := rows.Scan(
			&r.ID, &r.Amount, &r.Status, &pixKey, &pixKeyType, &holderName,
			&note, &adminNote, &r.CreatedAt, &reviewedAt, &paidAt,
		); err != nil {
			log.Printf("GetPayoutRequestsHandler scan: %v", err)
			continue
		}
		r.PixKey = ptr(pixKey)
		r.PixKeyType = ptr(pixKeyType)
		r.HolderName = ptr(holderName)
		r.Note = ptr(note)
		r.AdminNote = ptr(adminNote)
		r.ReviewedAt = ptrTime(reviewedAt)
		r.PaidAt = ptrTime(paidAt)
		list = append(list, r)
	}

	c.JSON(http.StatusOK, gin.H{"payouts": list})
}

// CreatePayoutRequest é o corpo de solicitação de repasse.
type CreatePayoutRequest struct {
	Amount        float64 `json:"amount" binding:"required"`
	Note          string  `json:"note"`
	BankAccountID string  `json:"bank_account_id"`
}

// CreatePayoutRequestHandler — POST /org/:slug/payout-requests
// Guarda um snapshot da conta/chave Pix para o repasse não mudar depois.
func CreatePayoutRequestHandler(c *gin.Context) {
	orgID, ok := resolveOrgMember(c)
	if !ok {
		return
	}

	var req CreatePayoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valor deve ser maior que zero"})
		return
	}

	db := config.GetDB()
	ctx := context.Background()

	balance, err := payoutservice.OrgBalance(db, orgID)
	if err != nil {
		log.Printf("CreatePayoutRequest balance: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao calcular saldo"})
		return
	}
	if req.Amount > balance.Available {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "valor maior que o saldo disponível",
			"available": balance.Available,
		})
		return
	}

	accountID := req.BankAccountID
	query := `SELECT holder_name, holder_document, pix_key, pix_key_type
	            FROM organization_bank_accounts
	           WHERE organization_id = $1 AND is_default = true`
	args := []interface{}{orgID}
	if accountID != "" {
		query = `SELECT holder_name, holder_document, pix_key, pix_key_type
		           FROM organization_bank_accounts
		          WHERE organization_id = $1 AND id = $2`
		args = append(args, accountID)
	}

	var holderName, holderDocument, pixKey, pixKeyType sql.NullString
	err = db.QueryRowContext(ctx, query, args...).
		Scan(&holderName, &holderDocument, &pixKey, &pixKeyType)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cadastre uma conta ou chave Pix antes de solicitar o repasse"})
		return
	} else if err != nil {
		log.Printf("CreatePayoutRequest account: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar conta Pix"})
		return
	}
	if !pixKey.Valid || pixKey.String == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a conta selecionada não tem chave Pix cadastrada"})
		return
	}

	if accountID == "" {
		_ = db.QueryRowContext(ctx,
			`SELECT id FROM organization_bank_accounts
			  WHERE organization_id = $1 AND is_default = true`, orgID,
		).Scan(&accountID)
	}

	requestedBy, _ := c.Get("userID")
	uid, _ := requestedBy.(string)

	var id string
	err = db.QueryRowContext(ctx, `
		INSERT INTO payout_requests
		  (organization_id, requested_by, amount, status,
		   bank_account_id, pix_key, pix_key_type, holder_name, holder_document, note)
		VALUES ($1,$2,$3,'pending',
		        NULLIF($4,'')::uuid,
		        $5, $6, $7, $8,
		        NULLIF($9,''))
		RETURNING id`,
		orgID, uid, req.Amount,
		accountID,
		nullIfEmpty(pixKey), nullIfEmpty(pixKeyType),
		nullIfEmpty(holderName), nullIfEmpty(holderDocument),
		req.Note,
	)
	if err != nil {
		log.Printf("CreatePayoutRequest insert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar solicitação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": id, "message": "solicitação enviada"})
}
