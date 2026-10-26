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
	Update(ctx context.Context, id int64, u ProfileUpdate) (Account, error)
	ListByIDs(ctx context.Context, ids []int64) ([]User, error)
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

// UpdateProfile применяет частичное обновление. Пустой патч не трогает БД
// и возвращает текущий профиль.
func (s *Service) UpdateProfile(ctx context.Context, id int64, u ProfileUpdate) (User, error) {
	u = u.Normalize()
	if err := u.Validate(); err != nil {
		return User{}, fmt.Errorf("user.Service.UpdateProfile: validate: %w", err)
	}
	var (
		acc Account
		err error
	)
	if u.IsEmpty() {
		acc, err = s.store.GetByID(ctx, id)
	} else {
		acc, err = s.store.Update(ctx, id, u)
	}
	if err != nil {
		return User{}, fmt.Errorf("user.Service.UpdateProfile: user %d: %w", id, err)
	}
	return acc.User, nil
}

// ListBriefs отдаёт пользователей по списку id для модулей, которым нужны
// публичные сведения (участники, команды). Дубли и пустой список допустимы.
func (s *Service) ListBriefs(ctx context.Context, ids []int64) ([]User, error) {
	uniq := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			uniq = append(uniq, id)
		}
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	users, err := s.store.ListByIDs(ctx, uniq)
	if err != nil {
		return nil, fmt.Errorf("user.Service.ListBriefs: %w", err)
	}
	return users, nil
}
