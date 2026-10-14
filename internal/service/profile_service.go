// Package service holds business rules. It depends on small interfaces it
// defines itself (consumer-side), which is what makes the later move to a
// hexagonal layout cheap: those interfaces already are the ports.
package service

import (
	"context"

	"github.com/demispreviotto/gymproject-backend/internal/domain"
)

type ProfileStore interface {
	GetByID(ctx context.Context, id string) (domain.Profile, error)
}

type ProfileService struct {
	store ProfileStore
}

func NewProfileService(store ProfileStore) *ProfileService {
	return &ProfileService{store: store}
}

func (s *ProfileService) Me(ctx context.Context, userID string) (domain.Profile, error) {
	return s.store.GetByID(ctx, userID)
}
