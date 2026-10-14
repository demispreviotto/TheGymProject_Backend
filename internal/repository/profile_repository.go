// Package repository implements data access. It is the only layer that knows SQL.
package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/demispreviotto/gymproject-backend/internal/domain"
)

type ProfileRepository struct {
	pool *pgxpool.Pool
}

func NewProfileRepository(pool *pgxpool.Pool) *ProfileRepository {
	return &ProfileRepository{pool: pool}
}

func (r *ProfileRepository) GetByID(ctx context.Context, id string) (domain.Profile, error) {
	const q = `
		SELECT id::text, name, email, role::text, tenant_id::text, is_active
		FROM public.profiles
		WHERE id = $1`

	var p domain.Profile
	var role string
	err := r.pool.QueryRow(ctx, q, id).Scan(&p.ID, &p.Name, &p.Email, &role, &p.TenantID, &p.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Profile{}, err
	}
	p.Role = domain.Role(role)
	return p, nil
}
