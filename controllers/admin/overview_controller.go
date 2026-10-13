package admin

import (
	"log"
	"net/http"

	"bilheteria-api/config"
	"bilheteria-api/services/payoutservice"
	"github.com/gin-gonic/gin"
)

type OverviewResponse struct {
	Platform            payoutservice.PlatformMetrics `json:"platform"`
	Released            float64                       `json:"released"`
	InEscrow            float64                       `json:"in_escrow"`
	PendingPayouts      int                           `json:"pending_payouts"`
	PendingPayoutAmount float64                       `json:"pending_payout_amount"`
	TopOrgs             []payoutservice.OrgMetrics    `json:"top_orgs"`
}

// GetOverview devolve os KPIs do painel: arrecadação da plataforma, total no
// escrow/disponível e os repasses pendentes, além das orgs que mais geram taxa.
func GetOverview(c *gin.Context) {
	db := config.GetDB()

	platform, err := payoutservice.LoadPlatformMetrics(db)
	if err != nil {
		log.Printf("GetOverview platform: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar métricas"})
		return
	}

	orgs, err := payoutservice.ListOrgMetrics(db)
	if err != nil {
		log.Printf("GetOverview orgs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao carregar organizações"})
		return
	}

	var released, inEscrow float64
	for _, o := range orgs {
		released += o.Released
		inEscrow += o.InEscrow
	}

	var pendingCount int
	var pendingAmount float64
	if err := db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(amount), 0)
		FROM payout_requests WHERE status = 'pending'`,
	).Scan(&pendingCount, &pendingAmount); err != nil {
		log.Printf("GetOverview payouts: %v", err)
	}

	// ListOrgMetrics já ordena por taxa arrecadada (DESC); limitamos ao topo.
	const topN = 5
	topOrgs := orgs
	if len(topOrgs) > topN {
		topOrgs = topOrgs[:topN]
	}

	c.JSON(http.StatusOK, OverviewResponse{
		Platform:            platform,
		Released:            released,
		InEscrow:            inEscrow,
		PendingPayouts:      pendingCount,
		PendingPayoutAmount: pendingAmount,
		TopOrgs:             topOrgs,
	})
}
