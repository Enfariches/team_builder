// Package pgtest поднимает одноразовый PostgreSQL в Docker для интеграционных
// тестов и накатывает на него миграции проекта.
package pgtest

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// New возвращает пул к свежей БД с применёнными миграциями. Без Docker тест
// пропускается, а не падает.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return NewWithMaxConns(t, 0)
}

// NewWithMaxConns ограничивает размер пула (0 — по умолчанию). Маленький пул
// ловит взаимные блокировки, когда транзакция держит соединение и просит
// второе из того же пула: на машине с большим числом ядер пула хватает и
// ошибка не проявляется.
func NewWithMaxConns(t *testing.T, maxConns int32) *pgxpool.Pool {
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
		t.Fatalf("connection string: %v", err)
	}
	migrate(t, dsn)

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func migrate(t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db for migrations: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	// путь считаем от этого файла, чтобы тесты любого пакета нашли миграции
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
	if err := goose.Up(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}
