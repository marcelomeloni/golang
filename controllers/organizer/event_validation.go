package organizer

import (
	"context"
	"database/sql"
	"log"
)

// eventBelongsToOrg confirma que o evento da URL pertence à organização do
// usuário. Sem essa checagem, um membro de uma org consegue ler e alterar
// (comunicados, check-in) eventos de outra org só adivinhando o id na URL.
//
// Erro de banco vira false de propósito: quem chama responde 404 e o evento
// não é encontrado é a resposta correta para quem não tem nada a ver com ele.
func eventBelongsToOrg(ctx context.Context, db *sql.DB, eventID, orgID string) bool {
	var exists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE id = $1 AND organization_id = $2)`,
		eventID, orgID,
	).Scan(&exists); err != nil {
		log.Printf("eventBelongsToOrg: %v", err)
		return false
	}
	return exists
}
