package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdb "builder/backend/internal/auth/sqlc/db"
)

type repository struct {
	q *authdb.Queries
}

func (r repository) Insert(ctx context.Context, t RefreshToken, userAgent string) (RefreshToken, error) {
	fam, err := uuid.Parse(t.FamilyID)
	if err != nil {
		return RefreshToken{}, fmt.Errorf("auth.repository.Insert: parse family id: %w", err)
	}
	row, err := r.q.InsertRefreshToken(ctx, authdb.InsertRefreshTokenParams{
		UserID: t.UserID, TokenHash: t.Hash, FamilyID: fam,
		ExpiresAt: t.ExpiresAt, UserAgent: userAgent,
	})
	if err != nil {
		return RefreshToken{}, fmt.Errorf("auth.repository.Insert: %w", err)
	}
	return toRefreshToken(row), nil
}

func (r repository) GetForUpdate(ctx context.Context, hash []byte) (RefreshToken, error) {
	row, err := r.q.GetRefreshTokenForUpdate(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshToken{}, ErrTokenNotFound
	}
	if err != nil {
		return RefreshToken{}, fmt.Errorf("auth.repository.GetForUpdate: %w", err)
	}
	return toRefreshToken(row), nil
}

func (r repository) Revoke(ctx context.Context, id int64, replacedBy *int64) error {
	err := r.q.RevokeRefreshToken(ctx, authdb.RevokeRefreshTokenParams{ID: id, ReplacedBy: replacedBy})
	if err != nil {
		return fmt.Errorf("auth.repository.Revoke: %w", err)
	}
	return nil
}

func (r repository) RevokeFamily(ctx context.Context, familyID string) error {
	fam, err := uuid.Parse(familyID)
	if err != nil {
		return fmt.Errorf("auth.repository.RevokeFamily: parse family id: %w", err)
	}
	if err := r.q.RevokeRefreshFamily(ctx, fam); err != nil {
		return fmt.Errorf("auth.repository.RevokeFamily: %w", err)
	}
	return nil
}

func toRefreshToken(t authdb.RefreshToken) RefreshToken {
	return RefreshToken{
		ID: t.ID, UserID: t.UserID, Hash: t.TokenHash,
		FamilyID: t.FamilyID.String(), ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt,
	}
}

func newFamilyID() string { return uuid.NewString() }

// PgTxRunner реализует TxRunner поверх пула pgx.
type PgTxRunner struct {
	pool *pgxpool.Pool
}

func NewPgTxRunner(pool *pgxpool.Pool) *PgTxRunner { return &PgTxRunner{pool: pool} }

func (p *PgTxRunner) Run(ctx context.Context, fn func(Store) error) (err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth.PgTxRunner.Run: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(repository{q: authdb.New(tx)}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth.PgTxRunner.Run: commit tx: %w", err)
	}
	return nil
}
