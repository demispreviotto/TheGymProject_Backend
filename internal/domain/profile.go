// Package domain holds entities and sentinel errors. It imports nothing from
// the rest of the project, so it can stay untouched if the layers are later
// rearranged into a hexagonal layout.
package domain

import "errors"

var ErrNotFound = errors.New("not found")

type Role string

const (
	RoleTrainer Role = "trainer"
	RoleUser    Role = "user"
	RoleFree    Role = "free"
	RoleAdmin   Role = "admin"
)

type Profile struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Email    string  `json:"email"`
	Role     Role    `json:"role"`
	TenantID *string `json:"tenant_id"`
	IsActive bool    `json:"is_active"`
}
