package invite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	invitedb "builder/backend/internal/invite/sqlc/db"
)

type Repository struct {
	q *invitedb.Queries
}

func NewRepository(db invitedb.DBTX) *Repository {
	return &Repository{q: invitedb.New(db)}
}

func (r *Repository) Create(ctx context.Context, splitID int64, token string, n NewInvite) (Invite, error) {
	var maxUses *int32
	if n.MaxUses != nil {
		v := int32(*n.MaxUses)
		maxUses = &v
	}
	row, err := r.q.InsertInvite(ctx, invitedb.InsertInviteParams{
		SplitID: splitID, Token: token, ExpiresAt: n.ExpiresAt, MaxUses: maxUses,
	})
	if err != nil {
		return Invite{}, fmt.Errorf("invite.repository.Create: %w", err)
	}
	return toInvite(row), nil
}

func (r *Repository) List(ctx context.Context, splitID int64) ([]Invite, error) {
	rows, err := r.q.ListInvitesBySplit(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("invite.repository.List: %w", err)
	}
	out := make([]Invite, len(rows))
	for i, row := range rows {
		out[i] = toInvite(row)
	}
	return out, nil
}

func (r *Repository) GetByToken(ctx context.Context, token string) (Invite, error) {
	row, err := r.q.GetInviteByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrNotFound
	}
	if err != nil {
		return Invite{}, fmt.Errorf("invite.repository.GetByToken: %w", err)
	}
	return toInvite(row), nil
}

// Revoke идемпотентен: повторный отзыв находит строку и не меняет revoked_at.
func (r *Repository) Revoke(ctx context.Context, splitID, id int64, now time.Time) error {
	n, err := r.q.RevokeInvite(ctx, invitedb.RevokeInviteParams{Now: now, ID: id, SplitID: splitID})
	if err != nil {
		return fmt.Errorf("invite.repository.Revoke: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Redeem увеличивает счётчик одним условным UPDATE; если условия
// действительности не выполнены, строк нет и возвращается ErrInviteInvalid.
func (r *Repository) Redeem(ctx context.Context, token string, now time.Time) (Redemption, error) {
	row, err := r.q.RedeemInvite(ctx, invitedb.RedeemInviteParams{Token: token, Now: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return Redemption{}, ErrInviteInvalid
	}
	if err != nil {
		return Redemption{}, fmt.Errorf("invite.repository.Redeem: %w", err)
	}
	return Redemption{InviteID: row.ID, SplitID: row.SplitID}, nil
}

func (r *Repository) Release(ctx context.Context, id int64) error {
	n, err := r.q.ReleaseInvite(ctx, id)
	if err != nil {
		return fmt.Errorf("invite.repository.Release: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func toInvite(row invitedb.Invite) Invite {
	var maxUses *int
	if row.MaxUses != nil {
		v := int(*row.MaxUses)
		maxUses = &v
	}
	return Invite{
		ID: row.ID, SplitID: row.SplitID, Token: row.Token,
		ExpiresAt: row.ExpiresAt, MaxUses: maxUses, UsesCount: int(row.UsesCount),
		RevokedAt: row.RevokedAt, CreatedAt: row.CreatedAt,
	}
}
