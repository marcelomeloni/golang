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

// validarCPF confere os dois dígitos verificadores do CPF.
// Aceita o CPF com ou sem pontuação; quem chama deve passar o resultado
// de cleanCPF, mas a função tolera a string formatada.
//
// Espelha o front (lib/authHelpers.ts → validateCPF): mesma fórmula, mesma
// rejeição de CPF com todos os dígitos iguais.
//
// Existe porque cleanCPF só checa comprimento — insuficiente para
// transferência de ingresso, que é irreversível e não tem como o
// destinatário reclamar depois de um CPF digitado errado.
func validarCPF(cpf string) bool {
	digits := nonDigitsRe.ReplaceAllString(cpf, "")
	if len(digits) != 11 {
		return false
	}

	nums := make([]int, 11)
	for i := 0; i < 11; i++ {
		nums[i] = int(digits[i] - '0')
	}

	// 000.000.000-00, 111.111.111-11, ... passam no cálculo mas não existem.
	repeated := true
	for i := 1; i < 11; i++ {
		if nums[i] != nums[0] {
			repeated = false
			break
		}
	}
	if repeated {
		return false
	}

	var sum int
	for i := 0; i < 9; i++ {
		sum += nums[i] * (10 - i)
	}
	if cpfCheckDigit(sum*10) != nums[9] {
		return false
	}

	sum = 0
	for i := 0; i < 10; i++ {
		sum += nums[i] * (11 - i)
	}
	return cpfCheckDigit(sum*10) == nums[10]
}

// cpfCheckDigit aplica a regra do dígito verificador: resto 10 vira 0.
func cpfCheckDigit(raw int) int {
	remainder := raw % 11
	if remainder == 10 {
		return 0
	}
	return remainder
}
