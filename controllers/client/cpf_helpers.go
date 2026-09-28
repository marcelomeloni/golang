package client

import "regexp"

// ==========================================
// HELPERS COMPARTILHADOS — CPF / TELEFONE
//
// Usados por: auth_controller.go, user_controller.go,
// profile_controller.go (CheckProfile/CompleteProfile).
//
// IMPORTANTE: se alguma dessas funções já existir em outro arquivo
// do pacote "client", REMOVA a duplicata de lá antes de commitar —
// Go não permite duas funções com o mesmo nome no mesmo pacote
// ("cleanCPF redeclared in this block").
// ==========================================

var nonDigitsRe = regexp.MustCompile(`\D`)

// cleanCPF remove tudo que não é dígito e valida que sobraram 11 dígitos.
func cleanCPF(raw string) (string, bool) {
	digits := nonDigitsRe.ReplaceAllString(raw, "")
	if len(digits) != 11 {
		return "", false
	}
	return digits, true
}

// formatCPF formata 11 dígitos como 000.000.000-00.
// Se não tiver exatamente 11 dígitos, devolve a string original sem alterar.
func formatCPF(digits string) string {
	if len(digits) != 11 {
		return digits
	}
	return digits[0:3] + "." + digits[3:6] + "." + digits[6:9] + "-" + digits[9:11]
}

// cleanPhone remove tudo que não é dígito de um número de telefone.
func cleanPhone(raw string) string {
	return nonDigitsRe.ReplaceAllString(raw, "")
}
