package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GET /org/:slug/bank-accounts
func GetBankAccountsHandler(c *gin.Context) {
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
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, holder_name, holder_document, bank_code, bank_name,
		        agency, agency_digit, account_number, account_digit,
		        account_type, pix_key, pix_key_type, is_default,
		        to_char(created_at, 'YYYY-MM-DD') AS created_at
		   FROM organization_bank_accounts
		  WHERE organization_id = $1
		  ORDER BY is_default DESC, created_at ASC`, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar contas"})
		return
	}
	defer rows.Close()

	type AccountRow struct {
		ID             string
		HolderName     string
		HolderDocument string
		BankCode       *string
		BankName       *string
		Agency         *string
		AgencyDigit    *string
		AccountNumber  *string
		AccountDigit   *string
		AccountType    *string
		PixKey         *string
		PixKeyType     *string
		IsDefault      bool
		CreatedAt      string
	}

	var accounts []gin.H
	for rows.Next() {
		var a AccountRow
		if err := rows.Scan(
			&a.ID, &a.HolderName, &a.HolderDocument, &a.BankCode, &a.BankName,
			&a.Agency, &a.AgencyDigit, &a.AccountNumber, &a.AccountDigit,
			&a.AccountType, &a.PixKey, &a.PixKeyType, &a.IsDefault, &a.CreatedAt,
		); err != nil {
			continue
		}
		accounts = append(accounts, gin.H{
			"id": a.ID, "holder_name": a.HolderName, "holder_document": a.HolderDocument,
			"bank_code": a.BankCode, "bank_name": a.BankName,
			"agency": a.Agency, "agency_digit": a.AgencyDigit,
			"account_number": a.AccountNumber, "account_digit": a.AccountDigit,
			"account_type": a.AccountType,
			"pix_key": a.PixKey, "pix_key_type": a.PixKeyType,
			"is_default": a.IsDefault, "created_at": a.CreatedAt,
		})
	}

	if accounts == nil {
		accounts = []gin.H{}
	}

	c.JSON(http.StatusOK, accounts)
}

// POST /org/:slug/bank-accounts
func AddBankAccountHandler(c *gin.Context) {
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
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var body struct {
		HolderName     string  `json:"holder_name"     binding:"required"`
		HolderDocument string  `json:"holder_document" binding:"required"`
		BankCode       *string `json:"bank_code"`
		BankName       *string `json:"bank_name"`
		Agency         *string `json:"agency"`
		AgencyDigit    *string `json:"agency_digit"`
		AccountNumber  *string `json:"account_number"`
		AccountDigit   *string `json:"account_digit"`
		AccountType    *string `json:"account_type"`
		PixKey         *string `json:"pix_key"`
		PixKeyType     *string `json:"pix_key_type"`
		IsDefault      bool    `json:"is_default"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	// Se for default, remove default das outras
	if body.IsDefault {
		_, err = tx.ExecContext(ctx,
			`UPDATE organization_bank_accounts SET is_default = false WHERE organization_id = $1`,
			orgID,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar contas"})
			return
		}
	}

	accountID := uuid.New().String()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO organization_bank_accounts
		   (id, organization_id, holder_name, holder_document, bank_code, bank_name,
		    agency, agency_digit, account_number, account_digit, account_type,
		    pix_key, pix_key_type, is_default)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		accountID, orgID, body.HolderName, body.HolderDocument,
		body.BankCode, body.BankName, body.Agency, body.AgencyDigit,
		body.AccountNumber, body.AccountDigit, body.AccountType,
		body.PixKey, body.PixKeyType, body.IsDefault,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao criar conta: " + err.Error()})
		return
	}

	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": accountID, "message": "conta cadastrada"})
}

// PATCH /org/:slug/bank-accounts/:accountID/default — define como padrão
func SetDefaultBankAccountHandler(c *gin.Context) {
	slug := c.Param("slug")
	accountID := c.Param("accountID")
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
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao iniciar transação"})
		return
	}
	defer tx.Rollback()

	_, _ = tx.ExecContext(ctx,
		`UPDATE organization_bank_accounts SET is_default = false WHERE organization_id = $1`, orgID,
	)
	_, err = tx.ExecContext(ctx,
		`UPDATE organization_bank_accounts SET is_default = true
		  WHERE id = $1 AND organization_id = $2`, accountID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao definir conta padrão"})
		return
	}

	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao confirmar transação"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "conta padrão atualizada"})
}

// DELETE /org/:slug/bank-accounts/:accountID
func DeleteBankAccountHandler(c *gin.Context) {
	slug := c.Param("slug")
	accountID := c.Param("accountID")
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
	if err != nil || role != "owner" {
		c.JSON(http.StatusForbidden, gin.H{"error": "apenas o owner pode remover contas bancárias"})
		return
	}

	_, err = db.ExecContext(ctx,
		`DELETE FROM organization_bank_accounts WHERE id = $1 AND organization_id = $2`,
		accountID, orgID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao remover conta"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "conta removida"})
}