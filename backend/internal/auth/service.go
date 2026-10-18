package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

// Store — операции с refresh-токенами внутри одной транзакции.
type Store interface {
	Insert(ctx context.Context, t RefreshToken, userAgent string) (RefreshToken, error)
	GetForUpdate(ctx context.Context, hash []byte) (RefreshToken, error) // ErrTokenNotFound
	Revoke(ctx context.Context, id int64, replacedBy *int64) error
	RevokeFamily(ctx context.Context, familyID string) error
}

// TxRunner открывает транзакцию по запросу сервиса: границу транзакции
// определяет service, репозиторий получает готовое соединение.
type TxRunner interface {
	Run(ctx context.Context, fn func(Store) error) error
}

type Service struct {
	users      UserStore
	tx         TxRunner
	tokens     *TokenIssuer
	refreshTTL time.Duration
	now        func() time.Time
	// dummyHash выравнивает время ответа login для несуществующего логина.
	dummyHash string
}

func NewService(users UserStore, tx TxRunner, tokens *TokenIssuer, refreshTTL time.Duration) (*Service, error) {
	pw := make([]byte, 16)
	if _, err := rand.Read(pw); err != nil {
		return nil, fmt.Errorf("dummy password: %w", err)
	}
	dummy, err := HashPassword(string(pw))
	if err != nil {
		return nil, fmt.Errorf("dummy hash: %w", err)
	}
	return &Service{
		users: users, tx: tx, tokens: tokens,
		refreshTTL: refreshTTL, now: time.Now, dummyHash: dummy,
	}, nil
}

// SetClock подменяет источник времени; нужен тестам истечения токенов.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func (s *Service) Register(ctx context.Context, in RegisterInput, userAgent string) (Session, error) {
	if err := in.Validate(); err != nil {
		return Session{}, fmt.Errorf("auth.Service.Register: validate input: %w", err)
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Register: hash password: %w", err)
	}
	u, err := s.users.Create(ctx, NewUser{
		Login: in.Login, PasswordHash: hash,
		FirstName: in.FirstName, MiddleName: in.MiddleName,
		LastName: in.LastName, Link: in.Link,
	})
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Register: create user: %w", err)
	}
	session, err := s.startSession(ctx, u, newFamilyID(), userAgent, nil)
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Register: %w", err)
	}
	return session, nil
}

func (s *Service) Login(ctx context.Context, login, password, userAgent string) (Session, error) {
	acc, err := s.users.GetByLogin(ctx, login)
	if errors.Is(err, ErrUserNotFound) {
		VerifyPassword(password, s.dummyHash)
		return Session{}, fmt.Errorf("auth.Service.Login: user %q not found: %w", login, ErrInvalidCredentials)
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Login: get user by login: %w", err)
	}
	if !VerifyPassword(password, acc.PasswordHash) {
		return Session{}, fmt.Errorf("auth.Service.Login: wrong password for user %d: %w", acc.ID, ErrInvalidCredentials)
	}
	session, err := s.startSession(ctx, acc.User, newFamilyID(), userAgent, nil)
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Login: %w", err)
	}
	return session, nil
}

// Refresh ротирует refresh-токен. Повторное предъявление уже отозванного
// токена означает, что его мог украсть кто-то второй, — отзываем всю цепочку.
func (s *Service) Refresh(ctx context.Context, refreshToken, userAgent string) (Session, error) {
	if refreshToken == "" {
		return Session{}, fmt.Errorf("auth.Service.Refresh: refresh cookie is empty: %w", ErrUnauthorized)
	}
	var (
		session Session
		reused  bool
	)
	err := s.tx.Run(ctx, func(st Store) error {
		old, err := st.GetForUpdate(ctx, HashRefreshToken(refreshToken))
		if errors.Is(err, ErrTokenNotFound) {
			return fmt.Errorf("refresh token not found: %w", ErrUnauthorized)
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if old.RevokedAt != nil {
			reused = true
			if err := st.RevokeFamily(ctx, old.FamilyID); err != nil {
				return fmt.Errorf("revoke family %s after reuse: %w", old.FamilyID, err)
			}
			return nil
		}
		if !old.ExpiresAt.After(s.now()) {
			return fmt.Errorf("refresh token %d expired: %w", old.ID, ErrUnauthorized)
		}
		u, err := s.users.GetByID(ctx, old.UserID)
		if errors.Is(err, ErrUserNotFound) {
			return fmt.Errorf("user %d of refresh token not found: %w", old.UserID, ErrUnauthorized)
		}
		if err != nil {
			return fmt.Errorf("get user %d: %w", old.UserID, err)
		}
		session, err = s.issue(ctx, st, u, old.FamilyID, userAgent, &old.ID)
		if err != nil {
			return fmt.Errorf("rotate token: %w", err)
		}
		return nil
	})
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.Refresh: %w", err)
	}
	if reused {
		return Session{}, fmt.Errorf("auth.Service.Refresh: revoked token reused, family revoked: %w", ErrUnauthorized)
	}
	return session, nil
}

// Logout идемпотентен: неизвестный или уже отозванный токен — не ошибка.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	err := s.tx.Run(ctx, func(st Store) error {
		t, err := st.GetForUpdate(ctx, HashRefreshToken(refreshToken))
		if errors.Is(err, ErrTokenNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if err := st.RevokeFamily(ctx, t.FamilyID); err != nil {
			return fmt.Errorf("revoke family %s: %w", t.FamilyID, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("auth.Service.Logout: %w", err)
	}
	return nil
}

func (s *Service) startSession(ctx context.Context, u User, familyID, userAgent string, replace *int64) (Session, error) {
	var session Session
	err := s.tx.Run(ctx, func(st Store) error {
		var err error
		session, err = s.issue(ctx, st, u, familyID, userAgent, replace)
		return err
	})
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.startSession: user %d: %w", u.ID, err)
	}
	return session, nil
}

func (s *Service) issue(ctx context.Context, st Store, u User, familyID, userAgent string, replace *int64) (Session, error) {
	access, err := s.tokens.Issue(u.ID)
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.issue: issue access token: %w", err)
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.issue: generate refresh token: %w", err)
	}
	expires := s.now().Add(s.refreshTTL)
	rec, err := st.Insert(ctx, RefreshToken{
		UserID: u.ID, Hash: hash, FamilyID: familyID, ExpiresAt: expires,
	}, userAgent)
	if err != nil {
		return Session{}, fmt.Errorf("auth.Service.issue: store refresh token: %w", err)
	}
	if replace != nil {
		if err := st.Revoke(ctx, *replace, &rec.ID); err != nil {
			return Session{}, fmt.Errorf("auth.Service.issue: revoke replaced token %d: %w", *replace, err)
		}
	}
	return Session{
		AccessToken: access, ExpiresIn: s.tokens.TTL(),
		RefreshToken: refresh, RefreshExpiresAt: expires, User: u,
	}, nil
}

// UserID стабилен на время запроса: кладётся middleware после проверки токена.
type ctxKey struct{}

func WithUserID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func UserID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(ctxKey{}).(int64)
	return id, ok
}

// Identity реализует user.IdentityProvider и аналогичные порты других модулей.
type Identity struct{}

func (Identity) UserID(ctx context.Context) (int64, bool) { return UserID(ctx) }
