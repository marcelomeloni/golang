package orgservice

import (
	"context"
	"database/sql"
	"fmt"
)

// ResolveOrgWithPermission verifica se o usuário tem role owner ou admin na
// organização identificada pelo slug e retorna o orgID.
// Retorna erro se o usuário não for membro ou não tiver permissão suficiente.
func ResolveOrgWithPermission(ctx context.Context, db *sql.DB, slug, uid string) (string, error) {
	var orgID, role string
	err := db.QueryRowContext(ctx,
		`SELECT o.id, om.role
		   FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`,
		slug, uid,
	).Scan(&orgID, &role)
	if err != nil {
		return "", fmt.Errorf("organização não encontrada ou usuário não é membro")
	}
	if role != "owner" && role != "admin" {
		return "", fmt.Errorf("permissão insuficiente: requer owner ou admin")
	}
	return orgID, nil
}

// ResolveOrgWithAnyMember retorna o orgID se o usuário for membro com qualquer role.
// Usar em rotas que qualquer membro pode acessar (ex: listar eventos).
func ResolveOrgWithAnyMember(ctx context.Context, db *sql.DB, slug, uid string) (string, error) {
	var orgID string
	err := db.QueryRowContext(ctx,
		`SELECT o.id
		   FROM organizations o
		   JOIN organization_members om ON om.organization_id = o.id
		  WHERE o.slug = $1 AND om.user_id = $2`,
		slug, uid,
	).Scan(&orgID)
	if err != nil {
		return "", fmt.Errorf("organização não encontrada ou usuário não é membro")
	}
	return orgID, nil
}

// IsMember verifica se o usuário pertence à organização com qualquer role.
func IsMember(ctx context.Context, db *sql.DB, slug, uid string) bool {
	var exists bool
	_ = db.QueryRowContext(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM organization_members om
		   JOIN organizations o ON o.id = om.organization_id
		  WHERE o.slug = $1 AND om.user_id = $2
		)`, slug, uid,
	).Scan(&exists)
	return exists
}

// GetMemberRole retorna o role do usuário na organização ou erro se não for membro.
func GetMemberRole(ctx context.Context, db *sql.DB, slug, uid string) (string, error) {
	var role string
	err := db.QueryRowContext(ctx,
		`SELECT om.role
		   FROM organization_members om
		   JOIN organizations o ON o.id = om.organization_id
		  WHERE o.slug = $1 AND om.user_id = $2`,
		slug, uid,
	).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("usuário não é membro desta organização")
	}
	return role, nil
}