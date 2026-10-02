package orderservice

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateQRCode gera um código único de ingresso no formato RPY-<HEX>.
func GenerateQRCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("RPY-%s", strings.ToUpper(hex.EncodeToString(b))), nil
}