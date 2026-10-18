package tests

import (
	"builder/backend/internal/auth"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeUsers struct {
	byLogin map[string]auth.UserWithHash
	nextID  int64
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byLogin: map[string]auth.UserWithHash{}} }

func (f *fakeUsers) Create(_ context.Context, n auth.NewUser) (auth.User, error) {
	if _, ok := f.byLogin[n.Login]; ok {
		return auth.User{}, auth.ErrLoginTaken
	}
	f.nextID++
	u := auth.User{ID: f.nextID, Login: n.Login, FirstName: n.FirstName, LastName: n.LastName}
	f.byLogin[n.Login] = auth.UserWithHash{User: u, PasswordHash: n.PasswordHash}
	return u, nil
}

func (f *fakeUsers) GetByLogin(_ context.Context, login string) (auth.UserWithHash, error) {
	u, ok := f.byLogin[login]
	if !ok {
		return auth.UserWithHash{}, auth.ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUsers) GetByID(_ context.Context, id int64) (auth.User, error) {
	for _, u := range f.byLogin {
		if u.ID == id {
			return u.User, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}

// memStore — in-memory auth.Store и TxRunner одновременно; мьютекс имитирует
// SELECT ... FOR UPDATE (транзакции выполняются последовательно).
type memStore struct {
	mu     sync.Mutex
	tokens []*auth.RefreshToken
}

func (m *memStore) Run(_ context.Context, fn func(auth.Store) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(m)
}

func (m *memStore) Insert(_ context.Context, t auth.RefreshToken, _ string) (auth.RefreshToken, error) {
	t.ID = int64(len(m.tokens) + 1)
	m.tokens = append(m.tokens, &t)
	return t, nil
}

func (m *memStore) GetForUpdate(_ context.Context, hash []byte) (auth.RefreshToken, error) {
	for _, t := range m.tokens {
		if string(t.Hash) == string(hash) {
			return *t, nil
		}
	}
	return auth.RefreshToken{}, auth.ErrTokenNotFound
}

func (m *memStore) Revoke(_ context.Context, id int64, _ *int64) error {
	for _, t := range m.tokens {
		if t.ID == id && t.RevokedAt == nil {
			now := time.Now()
			t.RevokedAt = &now
		}
	}
	return nil
}

func (m *memStore) RevokeFamily(_ context.Context, fam string) error {
	for _, t := range m.tokens {
		if t.FamilyID == fam && t.RevokedAt == nil {
			now := time.Now()
			t.RevokedAt = &now
		}
	}
	return nil
}

func (m *memStore) alive(fam string) int {
	n := 0
	for _, t := range m.tokens {
		if t.FamilyID == fam && t.RevokedAt == nil {
			n++
		}
	}
	return n
}

func newTestService(t *testing.T) (*auth.Service, *memStore, *fakeUsers) {
	t.Helper()
	users, store := newFakeUsers(), &memStore{}
	svc, err := auth.NewService(users, store, auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, users
}

var validReg = auth.RegisterInput{Login: "ivan", Password: "12345678", FirstName: "Иван", LastName: "Иванов"}

func TestRegisterAndLogin(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()

	s, err := svc.Register(ctx, validReg, "ua")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if s.AccessToken == "" || s.RefreshToken == "" || s.ExpiresIn != 15*time.Minute {
		t.Fatalf("неполная сессия: %+v", s)
	}
	if _, err := svc.Register(ctx, validReg, "ua"); !errors.Is(err, auth.ErrLoginTaken) {
		t.Fatalf("повторная регистрация: %v, want auth.ErrLoginTaken", err)
	}

	tests := []struct {
		name, login, password string
		wantErr               error
	}{
		{"верные данные", "ivan", "12345678", nil},
		{"неверный пароль", "ivan", "wrong-pass", auth.ErrInvalidCredentials},
		{"нет такого логина", "nobody", "12345678", auth.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Login(ctx, tt.login, tt.password, "ua")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Login err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterValidation(t *testing.T) {
	svc, _, users := newTestService(t)
	in := validReg
	in.Login = "x"
	_, err := svc.Register(context.Background(), in, "")
	var ve *auth.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if len(users.byLogin) != 0 {
		t.Fatal("пользователь создан несмотря на ошибку валидации")
	}
}

func TestRefreshRotation(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	s1, _ := svc.Register(ctx, validReg, "ua")

	s2, err := svc.Refresh(ctx, s1.RefreshToken, "ua")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if s2.RefreshToken == s1.RefreshToken {
		t.Fatal("refresh не ротирован")
	}
	fam := store.tokens[0].FamilyID
	if got := store.alive(fam); got != 1 {
		t.Fatalf("живых токенов в семье = %d, want 1", got)
	}

	// повтор старого токена: ошибка и отзыв всей семьи
	if _, err := svc.Refresh(ctx, s1.RefreshToken, "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("reuse err = %v, want auth.ErrUnauthorized", err)
	}
	if got := store.alive(fam); got != 0 {
		t.Fatalf("после reuse живых токенов = %d, want 0", got)
	}
	if _, err := svc.Refresh(ctx, s2.RefreshToken, "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("токен из отозванной семьи принят: %v", err)
	}
}

func TestRefreshRejects(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	s, _ := svc.Register(ctx, validReg, "ua")

	if _, err := svc.Refresh(ctx, "", "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("пустой токен: %v", err)
	}
	if _, err := svc.Refresh(ctx, "unknown", "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("неизвестный токен: %v", err)
	}
	svc.SetClock(func() time.Time { return time.Now().Add(31 * 24 * time.Hour) })
	if _, err := svc.Refresh(ctx, s.RefreshToken, "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("просроченный токен: %v", err)
	}
}

func TestRefreshConcurrentOnlyOneWins(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := context.Background()
	s, _ := svc.Register(ctx, validReg, "ua")

	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.Refresh(ctx, s.RefreshToken, "ua"); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("успешных refresh = %d, want 1", ok)
	}
}

func TestLogout(t *testing.T) {
	svc, store, _ := newTestService(t)
	ctx := context.Background()
	s, _ := svc.Register(ctx, validReg, "ua")

	if err := svc.Logout(ctx, s.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if got := store.alive(store.tokens[0].FamilyID); got != 0 {
		t.Fatalf("после logout живых токенов = %d", got)
	}
	if _, err := svc.Refresh(ctx, s.RefreshToken, "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("refresh после logout: %v", err)
	}
	for _, tok := range []string{"", "unknown", s.RefreshToken} {
		if err := svc.Logout(ctx, tok); err != nil {
			t.Fatalf("logout(%q) не идемпотентен: %v", tok, err)
		}
	}
}
