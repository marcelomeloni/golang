package organizer

import (
	"context"
	"net/http"

	"bilheteria-api/config"
	"github.com/gin-gonic/gin"
)

func GetProfileHandler(c *gin.Context) {
	userID, _ := c.Get("userID")
	uid := userID.(string)
	db := config.GetDB()

	type Row struct {
		ID        string
		FullName  *string
		Email     *string
		CPF       *string
		Phone     *string
		AvatarURL *string
	}
	var r Row
	err := db.QueryRowContext(context.Background(),
		`SELECT id, full_name, email, cpf, phone, avatar_url FROM users WHERE id = $1`, uid,
	).Scan(&r.ID, &r.FullName, &r.Email, &r.CPF, &r.Phone, &r.AvatarURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "usuário não encontrado"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         r.ID,
		"full_name":  r.FullName,
		"email":      r.Email,
		"cpf":        r.CPF,
		"phone":      r.Phone,
		"avatar_url": r.AvatarURL,
	})
}

func UpdateProfileHandler(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "usuário não autenticado"})
		return
	}
	uid := userID.(string)

	var body struct {
		CPF   *string `json:"cpf"`
		Phone *string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if body.CPF == nil && body.Phone == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "informe cpf ou telefone"})
		return
	}

	var cpfValue *string
	if body.CPF != nil {
		cpf := sanitizeCPF(*body.CPF)
		if !isValidCPF(cpf) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "CPF inválido"})
			return
		}
		cpfValue = &cpf
	}

	var phoneValue *string
	if body.Phone != nil {
		phone := sanitizePhone(*body.Phone)
		if len(phone) < 10 || len(phone) > 11 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Telefone inválido"})
			return
		}
		phoneValue = &phone
	}

	_, err := config.GetDB().ExecContext(context.Background(),
		`UPDATE users
		    SET cpf   = COALESCE($1, cpf),
		        phone = COALESCE($2, phone),
		        updated_at = now()
		  WHERE id = $3`,
		cpfValue, phoneValue, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "erro ao atualizar perfil: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "perfil atualizado"})
}

// sanitizeCPF remove pontos e traços, retornando apenas os 11 dígitos.
func sanitizeCPF(cpf string) string {
	result := make([]byte, 0, 11)
	for i := 0; i < len(cpf); i++ {
		if cpf[i] >= '0' && cpf[i] <= '9' {
			result = append(result, cpf[i])
		}
	}
	return string(result)
}

func sanitizePhone(phone string) string {
	result := make([]byte, 0, 11)
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			result = append(result, phone[i])
		}
	}
	return string(result)
}

func isValidCPF(cpf string) bool {
	if len(cpf) != 11 || cpf == "00000000000" || cpf == "11111111111" ||
		cpf == "22222222222" || cpf == "33333333333" || cpf == "44444444444" ||
		cpf == "55555555555" || cpf == "66666666666" || cpf == "77777777777" ||
		cpf == "88888888888" || cpf == "99999999999" {
		return false
	}

	checkDigit := func(length int) byte {
		sum := 0
		for i := 0; i < length; i++ {
			sum += int(cpf[i]-'0') * (length + 1 - i)
		}
		rest := sum % 11
		if rest < 2 {
			return '0'
		}
		return byte('0' + 11 - rest)
	}

	return cpf[9] == checkDigit(9) && cpf[10] == checkDigit(10)
}