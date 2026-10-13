package admin

import (
	"database/sql"
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/payoutservice"
	"github.com/gin-gonic/gin"
)

// ==========================================
// HANDLERS — ORGANIZAÇÕES E EVENTOS
// ==========================================

// ListOrgs devolve todas as organizações com métricas financeiras e saldo.
func ListOrgs(c *gin.Context) {
	db := config.GetDB()

	orgs, err := payoutservice.ListOrgMetrics(db)
	if err != nil {
		log.Printf("ListOrgs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar organizações"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"orgs": orgs})
}

// OrgDetail é a resposta do detalhe de uma organização.
type OrgDetail struct {
	Org     payoutservice.OrgMetrics        `json:"org"`
	Balance payoutservice.OrgBalanceResult  `json:"balance"`
	Events  []payoutservice.EventMetrics    `json:"events"`
}

// GetOrg devolve detalhe + saldo + eventos de uma organização.
func GetOrg(c *gin.Context) {
	orgID := c.Param("id")
	db := config.GetDB()

	org, err := payoutservice.LoadOrgMetrics(db, orgID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "organização não encontrada"})
		return
	}
	if err != nil {
		log.Printf("OrgDetail metrics: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar organização"})
		return
	}

	balance, err := payoutservice.OrgBalance(db, orgID)
	if err != nil {
		log.Printf("OrgDetail balance: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao calcular saldo"})
		return
	}

	events, err := payoutservice.EventMetricsForOrg(db, orgID)
	if err != nil {
		log.Printf("OrgDetail events: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar eventos"})
		return
	}

	c.JSON(http.StatusOK, OrgDetail{Org: *org, Balance: balance, Events: events})
}

// ListOrgEvents devolve os eventos de uma organização com métricas.
func ListOrgEvents(c *gin.Context) {
	orgID := c.Param("id")

	events, err := payoutservice.EventMetricsForOrg(config.GetDB(), orgID)
	if err != nil {
		log.Printf("ListOrgEvents: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao listar eventos"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"events": events})
}

// EventDetail devolve o resumo financeiro de um evento.
func EventDetail(c *gin.Context) {
	eventID := c.Param("id")

	event, err := payoutservice.EventMetricsByID(config.GetDB(), eventID)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "evento não encontrado"})
		return
	}
	if err != nil {
		log.Printf("EventDetail: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar evento"})
		return
	}

	c.JSON(http.StatusOK, event)
}

type UpdateFeeRequest struct {
	// Percentual como número "humano": 8.00 = 8%. Faixa aceita: 0 a 100.
	Percentage float64 `json:"percentage"`
}

// UpdateOrgFee define o percentual de taxa de uma organização.
func UpdateOrgFee(c *gin.Context) {
	orgID := c.Param("id")

	var req UpdateFeeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "payload inválido"})
		return
	}
	if req.Percentage < 0 || req.Percentage > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "percentual deve estar entre 0 e 100"})
		return
	}

	db := config.GetDB()
	res, err := db.Exec(
		`UPDATE organizations SET platform_fee_percentage = $1, updated_at = NOW() WHERE id = $2`,
		req.Percentage, orgID,
	)
	if err != nil {
		log.Printf("UpdateOrgFee: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar taxa"})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "organização não encontrada"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":               "taxa atualizada",
		"platform_fee_percentage": req.Percentage,
	})
}
