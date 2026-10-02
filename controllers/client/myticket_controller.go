package client

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
	"github.com/skip2/go-qrcode"
)

func GetMyTickets(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	rows, err := config.GetDB().Query(myTicketsQuery, userID)
	if err != nil {
		log.Printf("GetMyTickets query: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao buscar ingressos"})
		return
	}
	defer rows.Close()

	proximos, passados, err := groupTickets(rows)
	if err != nil {
		log.Printf("GetMyTickets scan: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao processar ingressos"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"proximos": proximos, "passados": passados})
}

func DownloadTicket(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "não autenticado"})
		return
	}

	row := config.GetDB().QueryRow(singleTicketQuery, c.Param("id"), userID)
	ticket, err := scanSingleTicket(row)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "ingresso não encontrado"})
		return
	}

	png, err := qrcode.Encode(ticket.QRCode, qrcode.Medium, 256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar QR"})
		return
	}

	tmplBytes, err := os.ReadFile("templates/ticket.html")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "template não encontrado"})
		return
	}

	tmpl, err := template.New("ticket").Parse(string(tmplBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro no parse do template"})
		return
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		MyTicket
		QRBase64 string
	}{ticket, base64.StdEncoding.EncodeToString(png)}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao gerar HTML"})
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Access-Control-Expose-Headers", "Content-Disposition")
	c.Header("Content-Disposition", fmt.Sprintf(`inline; filename="ingresso-%s.html"`, ticket.QRCode))
	c.String(http.StatusOK, buf.String())
}