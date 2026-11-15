package tests

import (
	"context"
	"errors"
	"testing"

	"builder/backend/internal/platform"
	"builder/backend/internal/platform/pgtest"
)

func TestTxManager(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE tx_probe (v INT)`); err != nil {
		t.Fatal(err)
	}
	m := platform.NewTxManager(pool)
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tx_probe`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	insert := func(ctx context.Context, v int) error {
		_, err := platform.DB(ctx, pool).Exec(ctx, `INSERT INTO tx_probe VALUES ($1)`, v)
		return err
	}
	boom := errors.New("boom")

	t.Run("commit", func(t *testing.T) {
		if err := m.Run(ctx, func(ctx context.Context) error { return insert(ctx, 1) }); err != nil {
			t.Fatal(err)
		}
		if count() != 1 {
			t.Fatalf("count = %d", count())
		}
	})

	t.Run("ошибка откатывает всё", func(t *testing.T) {
		before := count()
		err := m.Run(ctx, func(ctx context.Context) error {
			if err := insert(ctx, 2); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) || count() != before {
			t.Fatalf("err = %v, count = %d, want %d", err, count(), before)
		}
	})

	t.Run("вложенный Run делит транзакцию с внешним", func(t *testing.T) {
		before := count()
		err := m.Run(ctx, func(ctx context.Context) error {
			if err := m.Run(ctx, func(ctx context.Context) error { return insert(ctx, 4) }); err != nil {
				return err
			}
			// внутренний успех не коммитит: внешняя ошибка откатывает и его
			return boom
		})
		if !errors.Is(err, boom) || count() != before {
			t.Fatalf("err = %v, count = %d, want %d", err, count(), before)
		}
	})

}
