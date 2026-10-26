package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"builder/backend/internal/auth"
	"builder/backend/internal/platform/pgtest"
	"builder/backend/internal/user"
)

type userAdapter struct{ svc *user.Service }

func (a userAdapter) Create(ctx context.Context, u auth.NewUser) (auth.User, error) {
	acc, err := a.svc.Create(ctx, user.NewAccount{Login: u.Login, PasswordHash: u.PasswordHash,
		FirstName: u.FirstName, MiddleName: u.MiddleName, LastName: u.LastName, Link: u.Link})
	if errors.Is(err, user.ErrLoginTaken) {
		return auth.User{}, auth.ErrLoginTaken
	}
	return auth.User{ID: acc.ID, Login: acc.Login}, err
}

func (a userAdapter) GetByLogin(ctx context.Context, login string) (auth.UserWithHash, error) {
	acc, err := a.svc.GetByLogin(ctx, login)
	if errors.Is(err, user.ErrNotFound) {
		return auth.UserWithHash{}, auth.ErrUserNotFound
	}
	return auth.UserWithHash{User: auth.User{ID: acc.ID, Login: acc.Login}, PasswordHash: acc.PasswordHash}, err
}

func (a userAdapter) GetByID(ctx context.Context, id int64) (auth.User, error) {
	acc, err := a.svc.GetByID(ctx, id)
	if errors.Is(err, user.ErrNotFound) {
		return auth.User{}, auth.ErrUserNotFound
	}
	return auth.User{ID: acc.ID, Login: acc.Login}, err
}

func TestSessionLifecycleOnPostgres(t *testing.T) {
	// пул из двух соединений: если Refresh берёт второе соединение внутри
	// транзакции, параллельные запросы зависают (регрессия взаимной блокировки)
	pool := pgtest.NewWithMaxConns(t, 2)
	ctx := context.Background()
	svc, err := auth.NewService(userAdapter{user.NewService(user.NewRepository(pool))},
		auth.NewPgTxRunner(pool), auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute), 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	s, err := svc.Register(ctx, auth.RegisterInput{Login: "ivan", Password: "12345678", FirstName: "Иван", LastName: "Иванов"}, "ua")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, auth.RegisterInput{Login: "ivan", Password: "12345678", FirstName: "И", LastName: "И"}, "ua"); !errors.Is(err, auth.ErrLoginTaken) {
		t.Fatalf("дубль логина: %v", err)
	}

	// гонка: один refresh-токен предъявляют параллельно, выжить может ровно один
	const n = 6
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins []auth.Session
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if ns, err := svc.Refresh(rctx, s.RefreshToken, "ua"); err == nil {
				mu.Lock()
				wins = append(wins, ns)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// Победитель мог быть один; проигравшие, увидев отозванный токен,
	// отзывают семью — поэтому токен победителя уже может не работать.
	if len(wins) != 1 {
		t.Fatalf("успешных refresh = %d, want 1", len(wins))
	}

	var alive int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens WHERE revoked_at IS NULL`).Scan(&alive)
	if err != nil {
		t.Fatal(err)
	}
	if alive > 1 {
		t.Fatalf("живых refresh-токенов = %d, want <= 1", alive)
	}

	if err := svc.Logout(ctx, wins[0].RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(ctx, wins[0].RefreshToken, "ua"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("refresh после logout: %v", err)
	}
}
