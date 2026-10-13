// Package supabaseauth concentra o acesso mínimo à REST API de autenticação
// do Supabase (via net/http), usado pelos controllers de cliente e de admin.
//
// Mantido separado para evitar dependência do SDK e compartilhar a troca de
// credenciais por senha (grant_type=password) entre os dois painéis.
package supabaseauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Msg              string `json:"msg"`
	Message          string `json:"message"`
}

// ErrMessage extrai a mensagem legível de um corpo de erro do Supabase.
func ErrMessage(body []byte) string {
	var e errorResponse
	if err := json.Unmarshal(body, &e); err == nil {
		switch {
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Msg != "":
			return e.Msg
		case e.Message != "":
			return e.Message
		case e.Error != "":
			return e.Error
		}
	}
	return string(body)
}

// URL devolve a base do projeto Supabase (sem barra final).
func URL() string {
	return strings.TrimRight(os.Getenv("SUPABASE_URL"), "/")
}

// AnonKey devolve a chave anônima pública do projeto.
func AnonKey() string {
	return os.Getenv("SUPABASE_ANON_KEY")
}

// Session é o resultado de uma autenticação bem-sucedida.
type Session struct {
	AccessToken  string
	RefreshToken string
	UserID       string
	Email        string
}

type user struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         *user  `json:"user"`
}

// SignInWithPassword autentica por e-mail + senha contra o Supabase Auth.
// Retorna o status HTTP do Supabase em caso de erro (para diferenciar
// credenciais inválidas de falhas de infraestrutura).
func SignInWithPassword(email, password string) (Session, int, error) {
	b, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})

	req, err := http.NewRequest(
		http.MethodPost,
		URL()+"/auth/v1/token?grant_type=password",
		bytes.NewReader(b),
	)
	if err != nil {
		return Session{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", AnonKey())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Session{}, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return Session{}, resp.StatusCode, fmt.Errorf("%s", ErrMessage(body))
	}

	var out authResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return Session{}, resp.StatusCode, err
	}
	if out.User == nil {
		return Session{}, resp.StatusCode, fmt.Errorf("resposta sem usuário")
	}

	return Session{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		UserID:       out.User.ID,
		Email:        out.User.Email,
	}, resp.StatusCode, nil
}
