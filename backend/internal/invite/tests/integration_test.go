package tests

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/invite"
	"builder/backend/internal/platform/pgtest"
)

// newPgSplit создаёт владельца и пространство напрямую SQL-ом: чужие таблицы
// нужны тесту только как предпосылка для внешнего ключа.
func newPgSplit(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	var uid, sid int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (login, password_hash, first_name, last_name) VALUES ('owner', 'h', 'Имя', 'Фамилия') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO splits (owner_id, title, team_size) VALUES ($1, 'Практикум', 5) RETURNING id`, uid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	return sid
}

func TestRepositoryOnPostgres(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	repo := invite.NewRepository(pool)
	sid := newPgSplit(t, pool)
	now := time.Now().UTC().Truncate(time.Millisecond)

	inv, err := repo.Create(ctx, sid, "tok-a", invite.NewInvite{ExpiresAt: ptr(now.Add(time.Hour)), MaxUses: ptr(2)})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("запись читается обратно", func(t *testing.T) {
		got, err := repo.GetByToken(ctx, "tok-a")
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != inv.ID || got.MaxUses == nil || *got.MaxUses != 2 || got.ExpiresAt == nil || got.UsesCount != 0 || got.RevokedAt != nil {
			t.Fatalf("got = %+v", got)
		}
		if _, err := repo.GetByToken(ctx, "нет"); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("неизвестный токен: %v", err)
		}
	})

	t.Run("токен уникален", func(t *testing.T) {
		if _, err := repo.Create(ctx, sid, "tok-a", invite.NewInvite{}); err == nil {
			t.Fatal("дубль токена принят")
		}
	})

	t.Run("список: новые сверху", func(t *testing.T) {
		second, err := repo.Create(ctx, sid, "tok-b", invite.NewInvite{})
		if err != nil {
			t.Fatal(err)
		}
		list, err := repo.List(ctx, sid)
		if err != nil || len(list) != 2 || list[0].ID != second.ID {
			t.Fatalf("list=%+v err=%v", list, err)
		}
	})

	t.Run("истечение определяется параметром now", func(t *testing.T) {
		if _, err := repo.Redeem(ctx, "tok-a", now.Add(2*time.Hour)); !errors.Is(err, invite.ErrInviteInvalid) {
			t.Fatalf("истёкший: %v", err)
		}
		r, err := repo.Redeem(ctx, "tok-a", now)
		if err != nil || r.InviteID != inv.ID || r.SplitID != sid {
			t.Fatalf("r=%+v err=%v", r, err)
		}
	})

	t.Run("лимит использований", func(t *testing.T) {
		if _, err := repo.Redeem(ctx, "tok-a", now); err != nil {
			t.Fatalf("второе использование: %v", err)
		}
		if _, err := repo.Redeem(ctx, "tok-a", now); !errors.Is(err, invite.ErrInviteInvalid) {
			t.Fatalf("третье использование: %v", err)
		}
	})

	t.Run("Release возвращает использование и не уходит ниже нуля", func(t *testing.T) {
		if err := repo.Release(ctx, inv.ID); err != nil {
			t.Fatal(err)
		}
		if got, _ := repo.GetByToken(ctx, "tok-a"); got.UsesCount != 1 {
			t.Fatalf("uses = %d, want 1", got.UsesCount)
		}
		fresh, _ := repo.Create(ctx, sid, "tok-c", invite.NewInvite{})
		if err := repo.Release(ctx, fresh.ID); err != nil {
			t.Fatal(err)
		}
		if got, _ := repo.GetByToken(ctx, "tok-c"); got.UsesCount != 0 {
			t.Fatalf("uses = %d, want 0", got.UsesCount)
		}
		if err := repo.Release(ctx, 99999); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("несуществующий: %v", err)
		}
	})

	t.Run("отзыв идемпотентен и ограничен пространством", func(t *testing.T) {
		if err := repo.Revoke(ctx, sid, inv.ID, now); err != nil {
			t.Fatal(err)
		}
		if err := repo.Revoke(ctx, sid, inv.ID, now.Add(time.Hour)); err != nil {
			t.Fatalf("повторный отзыв: %v", err)
		}
		got, _ := repo.GetByToken(ctx, "tok-a")
		if got.RevokedAt == nil || !got.RevokedAt.Equal(now) {
			t.Fatalf("revoked_at = %v, want %v", got.RevokedAt, now)
		}
		if err := repo.Revoke(ctx, sid+1, inv.ID, now); !errors.Is(err, invite.ErrNotFound) {
			t.Fatalf("чужое пространство: %v", err)
		}
		if _, err := repo.Redeem(ctx, "tok-a", now); !errors.Is(err, invite.ErrInviteInvalid) {
			t.Fatalf("отозванный: %v", err)
		}
	})

	t.Run("каскад при удалении пространства", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DELETE FROM splits WHERE id = $1`, sid); err != nil {
			t.Fatal(err)
		}
		if list, _ := repo.List(ctx, sid); len(list) != 0 {
			t.Fatalf("инвайты остались: %+v", list)
		}
	})
}

// Пять параллельных попыток на единственное использование: успех ровно один.
// Пул из двух соединений ловит обращения к пулу изнутри транзакций.
func TestRedeemRace(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewWithMaxConns(t, 2)
	repo := invite.NewRepository(pool)
	sid := newPgSplit(t, pool)
	if _, err := repo.Create(ctx, sid, "last-seat", invite.NewInvite{MaxUses: ptr(1)}); err != nil {
		t.Fatal(err)
	}

	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if _, err := repo.Redeem(c, "last-seat", time.Now()); err == nil {
				wins.Add(1)
			} else if !errors.Is(err, invite.ErrInviteInvalid) {
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("успешных Redeem = %d, want 1", wins.Load())
	}
	if got, _ := repo.GetByToken(ctx, "last-seat"); got.UsesCount != 1 {
		t.Fatalf("uses_count = %d, want 1", got.UsesCount)
	}
}
