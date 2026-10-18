package tests

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"builder/backend/internal/auth"
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

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16",
		tcpostgres.WithDatabase("test"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	if err != nil {
		t.Skipf("docker недоступен: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })

	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "../../../migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSessionLifecycleOnPostgres(t *testing.T) {
	pool := newPool(t)
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
			if ns, err := svc.Refresh(ctx, s.RefreshToken, "ua"); err == nil {
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
