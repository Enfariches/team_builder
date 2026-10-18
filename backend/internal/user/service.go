package user

import (
	"context"
	"fmt"
)

// store — то, что сервису нужно от репозитория; интерфейс упрощает юнит-тесты.
type store interface {
	Create(ctx context.Context, a NewAccount) (Account, error)
	GetByLogin(ctx context.Context, login string) (Account, error)
	GetByID(ctx context.Context, id int64) (Account, error)
}

type Service struct {
	store store
}

func NewService(s store) *Service {
	return &Service{store: s}
}

func (s *Service) Create(ctx context.Context, a NewAccount) (Account, error) {
	acc, err := s.store.Create(ctx, a)
	if err != nil {
		return Account{}, fmt.Errorf("user.Service.Create: %w", err)
	}
	return acc, nil
}

func (s *Service) GetByLogin(ctx context.Context, login string) (Account, error) {
	acc, err := s.store.GetByLogin(ctx, login)
	if err != nil {
		return Account{}, fmt.Errorf("user.Service.GetByLogin: %w", err)
	}
	return acc, nil
}

func (s *Service) GetByID(ctx context.Context, id int64) (Account, error) {
	acc, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Account{}, fmt.Errorf("user.Service.GetByID: %w", err)
	}
	return acc, nil
}
