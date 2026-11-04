package form

import (
	"context"
	"fmt"
)

// store — то, что сервису нужно от репозитория; интерфейс упрощает юнит-тесты.
// Транзакции не нужны: каждая операция — один запрос.
type store interface {
	Create(ctx context.Context, userID int64, n NewForm) (Form, error)
	Get(ctx context.Context, userID, id int64) (Form, error)
	List(ctx context.Context, userID int64) ([]Form, error)
	Update(ctx context.Context, userID, id int64, u Update) (Form, error)
	Delete(ctx context.Context, userID, id int64) error
}

type Service struct {
	store store
}

func NewService(s store) *Service {
	return &Service{store: s}
}

func (s *Service) Create(ctx context.Context, userID int64, n NewForm) (Form, error) {
	n = n.Normalize()
	if err := n.Validate(); err != nil {
		return Form{}, fmt.Errorf("form.Service.Create: validate: %w", err)
	}
	f, err := s.store.Create(ctx, userID, n)
	if err != nil {
		return Form{}, fmt.Errorf("form.Service.Create: %w", err)
	}
	return f, nil
}

// Get возвращает анкету только её владельцу; чужая неотличима от отсутствующей.
func (s *Service) Get(ctx context.Context, userID, id int64) (Form, error) {
	f, err := s.store.Get(ctx, userID, id)
	if err != nil {
		return Form{}, fmt.Errorf("form.Service.Get: %w", err)
	}
	return f, nil
}

func (s *Service) List(ctx context.Context, userID int64) ([]Form, error) {
	fs, err := s.store.List(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("form.Service.List: %w", err)
	}
	return fs, nil
}

// Update применяет частичное обновление. Пустой патч не трогает БД
// и возвращает текущую анкету.
func (s *Service) Update(ctx context.Context, userID, id int64, u Update) (Form, error) {
	u = u.Normalize()
	if err := u.Validate(); err != nil {
		return Form{}, fmt.Errorf("form.Service.Update: validate: %w", err)
	}
	var (
		f   Form
		err error
	)
	if u.IsEmpty() {
		f, err = s.store.Get(ctx, userID, id)
	} else {
		f, err = s.store.Update(ctx, userID, id, u)
	}
	if err != nil {
		return Form{}, fmt.Errorf("form.Service.Update: %w", err)
	}
	return f, nil
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	if err := s.store.Delete(ctx, userID, id); err != nil {
		return fmt.Errorf("form.Service.Delete: %w", err)
	}
	return nil
}

// Snapshot отдаёт содержимое анкеты для заморозки при вступлении в пространство.
// Им будет пользоваться модуль member через собственный порт.
func (s *Service) Snapshot(ctx context.Context, userID, id int64) (Snapshot, error) {
	f, err := s.store.Get(ctx, userID, id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("form.Service.Snapshot: %w", err)
	}
	return f.Snapshot(), nil
}
