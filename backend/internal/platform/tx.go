package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX совпадает с интерфейсом, который генерирует sqlc: его реализуют и пул,
// и транзакция.
type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type txKey struct{}

// TxManager открывает транзакцию, общую для нескольких модулей. Транзакция
// передаётся через context, поэтому в портах модулей нет pgx-типов, а каждый
// модуль по-прежнему пишет только в свои таблицы.
type TxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// Run выполняет fn в транзакции. Если ctx уже несёт транзакцию, fn
// выполняется в ней же, а commit и rollback остаются за внешним вызовом.
//
// Внутри fn нельзя ходить в БД через пул мимо ctx: это второе соединение при
// уже занятом первом, и при множестве параллельных вызовов пул опустеет.
func (m *TxManager) Run(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("platform.TxManager.Run: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			// WithoutCancel: откат нужен и после отмены запроса, иначе
			// соединение вернётся в пул с открытой транзакцией
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("platform.TxManager.Run: commit tx: %w", err)
	}
	return nil
}

// DB возвращает транзакцию из ctx, а если её нет — fallback (обычно пул).
func DB(ctx context.Context, fallback DBTX) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return fallback
}

// InTx сообщает, что ctx несёт транзакцию.
func InTx(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}
