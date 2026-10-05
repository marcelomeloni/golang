package organizer

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"bilheteria-api/config"
	"bilheteria-api/services/emailsender"
	"bilheteria-api/services/orgservice"
	"bilheteria-api/services/promoterservice"
	"github.com/gin-gonic/gin"
)

type EmitPromoterTicketsRequest struct {
	Items []promoterservice.EmitRequestItem `json:"items" binding:"required,min=1,dive"`
}

type EmittedTicketResponse struct {
	TicketID        string `json:"ticket_id"`
	QRCode          string `json:"qr_code"`
	RecipientUserID string `json:"recipient_user_id"`
	RecipientName   string `json:"recipient_name"`
	RecipientEmail  string `json:"recipient_email"`
	BatchName       string `json:"batch_name"`
	BatchType       string `json:"batch_type"`
}

type EmitPromoterTicketsResponse struct {
	OrderID        string                 `json:"order_id"`
	EmittedTickets []EmittedTicketResponse `json:"emitted_tickets"`
	DistributionID string                 `json:"distribution_id"`
}

func EmitPromoterTicketsHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	// 1. Resolve a org por qualquer membership: o gate de role (promoter,
	// admin ou owner) é validado logo abaixo. Usar ResolveOrgWithPermission
	// aqui rejeitaria o próprio promoter, pois ela só aceita owner/admin.
	orgID, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	// Check if user has promoter role or higher
	var role string
	err = db.QueryRowContext(ctx, `
		SELECT role FROM organization_members 
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, uid).Scan(&role)
	if err != nil || (role != "owner" && role != "admin" && role != "promoter") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente: requer role promoter, admin ou owner"})
		return
	}

	// 2. Verify event belongs to organization
	var exists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)
	`, eventID, orgID).Scan(&exists)
	if err != nil || !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}

	// 3. Parse request
	var req EmitPromoterTicketsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 4. Validate CPF format for each item
	for i, item := range req.Items {
		cpfDigits := strings.NewReplacer(".", "", "-", "", " ", "").Replace(item.CPF)
		if len(cpfDigits) != 11 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d: CPF inválido", i+1)})
			return
		}
		if item.Quantity <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d: quantidade deve ser maior que zero", i+1)})
			return
		}
		if strings.TrimSpace(item.LotID) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d: lot_id é obrigatório", i+1)})
			return
		}
	}

	// 5. Emit tickets
	emailSender := emailsender.New("")
	result, err := promoterservice.EmitPromoterTickets(
		ctx,
		db,
		orgID,
		eventID,
		uid,
		req.Items,
		emailSender,
	)
	if err != nil {
		// Check if it's a specific business error
		errMsg := err.Error()
		if strings.Contains(errMsg, "indisponível") || strings.Contains(errMsg, "disponível") || 
		   strings.Contains(errMsg, "máximo") || strings.Contains(errMsg, "não encontrado") ||
		   strings.Contains(errMsg, "inválido") {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao emitir ingressos: " + errMsg})
		return
	}

	// 6. Build response
	response := EmitPromoterTicketsResponse{
		OrderID:        result.OrderID,
		DistributionID: result.DistributionID,
		EmittedTickets: make([]EmittedTicketResponse, len(result.EmittedTickets)),
	}
	for i, t := range result.EmittedTickets {
		response.EmittedTickets[i] = EmittedTicketResponse{
			TicketID:        t.TicketID,
			QRCode:          t.QRCode,
			RecipientUserID: t.RecipientUserID,
			RecipientName:   t.RecipientName,
			RecipientEmail:  t.RecipientEmail,
			BatchName:       t.BatchName,
			BatchType:       t.BatchType,
		}
	}

	c.JSON(http.StatusCreated, response)
}

// GetPromoterCPFLookupHandler — GET /org/:slug/events/:id/promoter/cpf-lookup?cpf=...
//
// Usado pelo promoter para decidir, antes de emitir, se o CPF já está
// cadastrado. Se exists=true, os dados do titular já estão na base e o
// formulário não precisa pedir nome/e-mail/telefone. Se exists=false, a
// emissão cria um usuário guest e o formulário precisa coletar esses dados.
//
// cpf_digits é coluna gerada/indexada a partir de cpf, então a busca não
// varre a tabela (mesma semântica de TransferTicket).
func GetPromoterCPFLookupHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	// Mesmo gate de permissão da emissão: promoter, admin ou owner.
	// ResolveOrgWithAnyMember aceita o membership; o role é validado abaixo.
	orgID, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var role string
	err = db.QueryRowContext(ctx, `
		SELECT role FROM organization_members
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, uid).Scan(&role)
	if err != nil || (role != "owner" && role != "admin" && role != "promoter") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente: requer role promoter, admin ou owner"})
		return
	}

	cpfDigits := promoterservice.CleanCPF(c.Query("cpf"))
	if len(cpfDigits) != 11 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "CPF inválido"})
		return
	}

	var (
		fullName sql.NullString
		email    sql.NullString
		phone    sql.NullString
		isGuest  bool
	)
	err = db.QueryRowContext(ctx, `
		SELECT full_name, email, phone, COALESCE(is_guest, false)
		FROM users
		WHERE cpf_digits = $1
	`, cpfDigits).Scan(&fullName, &email, &phone, &isGuest)

	if err == sql.ErrNoRows {
		// CPF sem cadastro: a emissão vai exigir os dados do titular.
		c.JSON(http.StatusOK, gin.H{
			"exists":     false,
			"is_guest":   false,
			"full_name":  "",
			"email":      "",
			"phone":      "",
			"needs_data": true,
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao consultar CPF"})
		return
	}

	// needs_data=true quando falta algum campo essencial para o cadastro.
	needsData := !fullName.Valid || fullName.String == "" ||
		!email.Valid || email.String == "" ||
		!phone.Valid || phone.String == ""

	c.JSON(http.StatusOK, gin.H{
		"exists":     true,
		"is_guest":   isGuest,
		"full_name":  fullName.String,
		"email":      email.String,
		"phone":      phone.String,
		"needs_data": needsData,
	})
}

func GetPromoterDistributionsHandler(c *gin.Context) {
	orgSlug := c.Param("slug")
	eventID := c.Param("id")
	userID, _ := c.Get("userID")
	uid := userID.(string)

	db := config.GetDB()
	ctx := c.Request.Context()

	// Resolve por membership e valida o role logo abaixo (promoter/admin/owner).
	// ResolveOrgWithPermission não serve aqui: ela exige owner/admin.
	orgID, err := orgservice.ResolveOrgWithAnyMember(ctx, db, orgSlug, uid)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "acesso negado"})
		return
	}

	var role string
	err = db.QueryRowContext(ctx, `
		SELECT role FROM organization_members 
		WHERE organization_id = $1 AND user_id = $2
	`, orgID, uid).Scan(&role)
	if err != nil || (role != "owner" && role != "admin" && role != "promoter") {
		c.JSON(http.StatusForbidden, gin.H{"error": "permissão insuficiente"})
		return
	}

	rows, err := db.QueryContext(ctx, `
		SELECT 
			pd.id,
			pd.batch_id,
			tb.name as batch_name,
			COALESCE(pd.recipient_user_id, ''),
			COALESCE(u.full_name, pd.recipient_name, '') as recipient_name,
			COALESCE(u.email, pd.recipient_email, '') as recipient_email,
			COALESCE(u.cpf, pd.recipient_cpf, '') as recipient_cpf,
			COALESCE(pd.recipient_phone, '') as recipient_phone,
			pd.quantity,
			to_char(pd.created_at, 'YYYY-MM-DD"T"HH24:MI:SS') as created_at,
			t.id as ticket_id,
			t.qr_code,
			t.status as ticket_status
		FROM promoter_distributions pd
		JOIN ticket_batches tb ON tb.id = pd.batch_id
		LEFT JOIN users u ON u.id = pd.recipient_user_id
		LEFT JOIN LATERAL (
			SELECT t.id, t.qr_code, t.status
			  FROM tickets t
			  JOIN orders o ON o.id = t.order_id
			 WHERE o.event_id = pd.event_id
			   AND o.order_type = 'promoter'
			   AND o.user_id = pd.promoter_user_id
			   AND t.user_id = pd.recipient_user_id
			   AND t.batch_id = pd.batch_id
			 LIMIT 1
		) t ON TRUE
		WHERE pd.event_id = $1 AND pd.promoter_user_id = $2 AND pd.quantity > 0
		ORDER BY pd.created_at DESC
	`, eventID, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar distribuições"})
		return
	}
	defer rows.Close()

	type Distribution struct {
		ID              string `json:"id"`
		BatchID         string `json:"batch_id"`
		BatchName       string `json:"batch_name"`
		RecipientUserID string `json:"recipient_user_id"`
		RecipientName   string `json:"recipient_name"`
		RecipientEmail  string `json:"recipient_email"`
		RecipientCPF    string `json:"recipient_cpf"`
		RecipientPhone  string `json:"recipient_phone"`
		Quantity        int    `json:"quantity"`
		CreatedAt       string `json:"created_at"`
		TicketID        string `json:"ticket_id,omitempty"`
		QRCode          string `json:"qr_code,omitempty"`
		TicketStatus    string `json:"ticket_status,omitempty"`
	}

	var distributions []Distribution
	for rows.Next() {
		var d Distribution
		var ticketID, qrCode, ticketStatus sql.NullString
		if err := rows.Scan(
			&d.ID,
			&d.BatchID,
			&d.BatchName,
			&d.RecipientUserID,
			&d.RecipientName,
			&d.RecipientEmail,
			&d.RecipientCPF,
			&d.RecipientPhone,
			&d.Quantity,
			&d.CreatedAt,
			&ticketID,
			&qrCode,
			&ticketStatus,
		); err != nil {
			continue
		}
		if ticketID.Valid {
			d.TicketID = ticketID.String
			d.QRCode = qrCode.String
			d.TicketStatus = ticketStatus.String
		}
		distributions = append(distributions, d)
	}

	c.JSON(http.StatusOK, distributions)
}