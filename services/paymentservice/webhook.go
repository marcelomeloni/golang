package paymentservice

import (
	"errors"
	"fmt"
	"time"
)

// ErrWebhookNotFound indica que a chave consultada ainda não possui webhook
// cadastrado no C6 (HTTP 404).
var ErrWebhookNotFound = errors.New("webhook não cadastrado")

// WebhookInfo é o webhook Pix registrado para uma chave DICT.
type WebhookInfo struct {
	WebhookURL string     `json:"webhook_url"`
	Chave      string     `json:"chave"`
	Criacao    *time.Time `json:"criacao,omitempty"`
}

// WebhookListParams são os filtros aceitos pela consulta de webhooks.
type WebhookListParams struct {
	Inicio         *time.Time
	Fim            *time.Time
	PaginaAtual    int
	ItensPorPagina int
}

// WebhookPagination replica a paginação devolvida pelo C6.
type WebhookPagination struct {
	PaginaAtual          int `json:"pagina_atual"`
	ItensPorPagina       int `json:"itens_por_pagina"`
	QuantidadeDePaginas  int `json:"quantidade_de_paginas"`
	QuantidadeTotalItens int `json:"quantidade_total_de_itens"`
}

// WebhookListParameters replica os parâmetros aplicados na consulta.
type WebhookListParameters struct {
	Inicio    string            `json:"inicio,omitempty"`
	Fim       string            `json:"fim,omitempty"`
	Paginacao WebhookPagination `json:"paginacao"`
}

// WebhookList é o resultado da consulta de webhooks cadastrados.
type WebhookList struct {
	Webhooks   []WebhookInfo         `json:"webhooks"`
	Parametros WebhookListParameters `json:"parametros"`
}

// WebhookManager expõe o gerenciamento de webhooks Pix do gateway ativo.
type WebhookManager interface {
	Environment() string
	PixKey() string
	GetWebhook(chave string) (WebhookInfo, error)
	SetWebhook(chave, webhookURL string) (WebhookInfo, error)
	DeleteWebhook(chave string) error
	ListWebhooks(params WebhookListParams) (WebhookList, error)
}

// DefaultWebhookManager devolve o gerenciador de webhooks do gateway ativo
// quando ele suporta essa operação (C6 Bank).
func DefaultWebhookManager() (WebhookManager, bool) {
	wm, ok := Default.(WebhookManager)
	return wm, ok
}

// C6APIError representa um problema RFC 7807 retornado pelo C6.
type C6APIError struct {
	Status        int                      `json:"status"`
	Title         string                   `json:"title"`
	Type          string                   `json:"type"`
	Detail        string                   `json:"detail"`
	CorrelationID string                   `json:"correlationId"`
	Violacoes     []map[string]interface{} `json:"violacoes,omitempty"`
}

func (e *C6APIError) Error() string {
	msg := e.Detail
	if msg == "" {
		msg = e.Title
	}
	if msg == "" {
		msg = fmt.Sprintf("C6 retornou HTTP %d", e.Status)
	}
	if e.CorrelationID != "" {
		msg = fmt.Sprintf("%s (correlationId=%s)", msg, e.CorrelationID)
	}
	return msg
}
