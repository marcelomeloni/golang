package health

import (
	"net/http"

	"bilheteria-api/config"

	"github.com/gin-gonic/gin"
)

func Check(c *gin.Context) {
	if err := config.GetDB().Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "db": "unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
