package eventservice

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var nonAlphanumericRe = regexp.MustCompile(`[^a-z0-9]+`)

// GenerateUniqueSlug cria um slug legível a partir do título e garante unicidade
// consultando a tabela events. Tenta até 10 sufixos numéricos antes de usar UUID.
func GenerateUniqueSlug(ctx context.Context, db *sql.DB, title string) (string, error) {
	base := Slugify(title)
	if base == "" {
		base = "evento"
	}

	candidate := base
	for i := 1; i <= 10; i++ {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM events WHERE slug = $1)`, candidate,
		).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}

	// Último recurso: sufixo UUID curto para garantir unicidade
	return fmt.Sprintf("%s-%s", base, uuid.New().String()[:8]), nil
}

// Slugify normaliza um título para slug ASCII sem acentos e sem caracteres especiais.
func Slugify(s string) string {
	t := transform.Chain(
		norm.NFD,
		runes.Remove(runes.In(unicode.Mn)),
		norm.NFC,
	)
	result, _, _ := transform.String(t, strings.ToLower(s))
	return strings.Trim(nonAlphanumericRe.ReplaceAllString(result, "-"), "-")
}