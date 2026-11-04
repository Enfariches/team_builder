package invite

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// store — то, что сервису нужно от репозитория; интерфейс упрощает юнит-тесты.
// Транзакции не нужны: каждая операция — один запрос.
type store interface {
	Create(ctx context.Context, splitID int64, token string, n NewInvite) (Invite, error)
	List(ctx context.Context, splitID int64) ([]Invite, error)
	GetByToken(ctx context.Context, token string) (Invite, error)
	Revoke(ctx context.Context, splitID, id int64, now time.Time) error
	Redeem(ctx context.Context, token string, now time.Time) (Redemption, error)
	Release(ctx context.Context, id int64) error
}

type Service struct {
	store    store
	splits   SplitAccess
	info     SplitInfoProvider
	now      func() time.Time
	newToken func() (string, error)
}

func NewService(s store, splits SplitAccess, info SplitInfoProvider) *Service {
	return &Service{store: s, splits: splits, info: info, now: time.Now, newToken: newToken}
}

// SetClock подменяет часы в тестах на истечение.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetTokenGenerator подменяет генератор токенов в тестах.
func (s *Service) SetTokenGenerator(f func() (string, error)) { s.newToken = f }

func (s *Service) List(ctx context.Context, userID, splitID int64) ([]Invite, error) {
	if err := s.requireOwner(ctx, userID, splitID); err != nil {
		return nil, fmt.Errorf("invite.Service.List: %w", err)
	}
	list, err := s.store.List(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("invite.Service.List: %w", err)
	}
	return list, nil
}

func (s *Service) Create(ctx context.Context, userID, splitID int64, n NewInvite) (Invite, error) {
	if err := s.requireOwner(ctx, userID, splitID); err != nil {
		return Invite{}, fmt.Errorf("invite.Service.Create: %w", err)
	}
	if err := n.Validate(s.now()); err != nil {
		return Invite{}, fmt.Errorf("invite.Service.Create: validate: %w", err)
	}
	token, err := s.newToken()
	if err != nil {
		return Invite{}, fmt.Errorf("invite.Service.Create: %w", err)
	}
	inv, err := s.store.Create(ctx, splitID, token, n)
	if err != nil {
		return Invite{}, fmt.Errorf("invite.Service.Create: %w", err)
	}
	return inv, nil
}

// Revoke идемпотентен; инвайт чужого пространства неотличим от отсутствующего.
func (s *Service) Revoke(ctx context.Context, userID, splitID, id int64) error {
	if err := s.requireOwner(ctx, userID, splitID); err != nil {
		return fmt.Errorf("invite.Service.Revoke: %w", err)
	}
	if err := s.store.Revoke(ctx, splitID, id, s.now()); err != nil {
		return fmt.Errorf("invite.Service.Revoke: %w", err)
	}
	return nil
}

// Preview публичный: доступ даёт сам токен. Недействительный токен не
// раскрывает причину и не обращается к пространству.
func (s *Service) Preview(ctx context.Context, token string) (SplitInfo, error) {
	inv, err := s.store.GetByToken(ctx, token)
	if errors.Is(err, ErrNotFound) {
		return SplitInfo{}, fmt.Errorf("invite.Service.Preview: %w", ErrInviteInvalid)
	}
	if err != nil {
		return SplitInfo{}, fmt.Errorf("invite.Service.Preview: %w", err)
	}
	if !inv.ValidAt(s.now()) {
		return SplitInfo{}, fmt.Errorf("invite.Service.Preview: invite %d: %w", inv.ID, ErrInviteInvalid)
	}
	info, err := s.info.SplitInfo(ctx, inv.SplitID)
	if err != nil {
		return SplitInfo{}, fmt.Errorf("invite.Service.Preview: %w", err)
	}
	return info, nil
}

// Redeem атомарно проверяет инвайт и занимает одно использование. Им будет
// пользоваться модуль member через собственный порт.
func (s *Service) Redeem(ctx context.Context, token string) (Redemption, error) {
	r, err := s.store.Redeem(ctx, token, s.now())
	if err != nil {
		return Redemption{}, fmt.Errorf("invite.Service.Redeem: %w", err)
	}
	return r, nil
}

// Release возвращает использование, если вступление не состоялось.
func (s *Service) Release(ctx context.Context, inviteID int64) error {
	if err := s.store.Release(ctx, inviteID); err != nil {
		return fmt.Errorf("invite.Service.Release: %w", err)
	}
	return nil
}

func (s *Service) requireOwner(ctx context.Context, userID, splitID int64) error {
	acc, err := s.splits.Authorize(ctx, userID, splitID)
	if err != nil {
		return fmt.Errorf("authorize split %d: %w", splitID, err)
	}
	if acc != AccessOwner {
		return ErrForbidden
	}
	return nil
}
