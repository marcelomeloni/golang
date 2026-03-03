package dbutil

import "strings"

// NullableText retorna nil para strings vazias ou só espaços,
// compatível com COALESCE e colunas nullable no Postgres.
func NullableText(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// NullableJSON retorna nil para slices vazios,
// compatível com COALESCE em colunas jsonb nullable.
func NullableJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}

// StrVal desreferencia um *string com fallback para string vazia.
func StrVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// FloatVal desreferencia um *float64 com fallback para zero.
func FloatVal(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

// IntVal desreferencia um *int com fallback para zero.
func IntVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}