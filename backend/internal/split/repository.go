package split

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	splitdb "builder/backend/internal/split/sqlc/db"
)

type Repository struct {
	q *splitdb.Queries
}

func NewRepository(db splitdb.DBTX) *Repository {
	return &Repository{q: splitdb.New(db)}
}

func (r *Repository) Insert(ctx context.Context, n NewSplit) (Split, error) {
	row, err := r.q.InsertSplit(ctx, splitdb.InsertSplitParams{
		OwnerID:         n.OwnerID,
		Title:           n.Title,
		Description:     n.Description,
		TeamSize:        int32(n.TeamSize),
		MaxMembers:      toInt32Ptr(n.MaxMembers),
		RemainderPolicy: string(n.RemainderPolicy),
		AutoFinishAt:    n.AutoFinishAt,
	})
	if err != nil {
		return Split{}, fmt.Errorf("split.repository.Insert: %w", err)
	}
	roles, err := r.insertRoles(ctx, row.ID, n.Roles)
	if err != nil {
		return Split{}, fmt.Errorf("split.repository.Insert: %w", err)
	}
	return toSplit(row, roles), nil
}

func (r *Repository) Get(ctx context.Context, id int64) (Split, error) {
	row, err := r.q.GetSplit(ctx, id)
	return r.withRoles(ctx, "Get", row, err)
}

func (r *Repository) GetForUpdate(ctx context.Context, id int64) (Split, error) {
	row, err := r.q.GetSplitForUpdate(ctx, id)
	return r.withRoles(ctx, "GetForUpdate", row, err)
}

// Update пишет скалярные поля и, если переданы роли, заменяет их целиком.
func (r *Repository) Update(ctx context.Context, id int64, p Patch) (Split, error) {
	var policy *string
	if p.RemainderPolicy != nil {
		v := string(*p.RemainderPolicy)
		policy = &v
	}
	row, err := r.q.UpdateSplit(ctx, splitdb.UpdateSplitParams{
		ID:              id,
		Title:           p.Title,
		TeamSize:        toInt32Ptr(p.TeamSize),
		RemainderPolicy: policy,
		SetDescription:  p.Description.Set,
		Description:     p.Description.Value,
		SetMaxMembers:   p.MaxMembers.Set,
		MaxMembers:      toInt32Ptr(p.MaxMembers.Value),
		SetAutoFinishAt: p.AutoFinishAt.Set,
		AutoFinishAt:    p.AutoFinishAt.Value,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Split{}, ErrNotFound
	}
	if err != nil {
		return Split{}, fmt.Errorf("split.repository.Update: %w", err)
	}
	if p.Roles != nil {
		if err := r.q.DeleteRolesBySplit(ctx, id); err != nil {
			return Split{}, fmt.Errorf("split.repository.Update: delete roles: %w", err)
		}
		if _, err := r.insertRoles(ctx, id, *p.Roles); err != nil {
			return Split{}, fmt.Errorf("split.repository.Update: %w", err)
		}
	}
	return r.withRoles(ctx, "Update", row, nil)
}

func (r *Repository) SetStatus(ctx context.Context, id int64, status Status) (Split, error) {
	row, err := r.q.UpdateSplitStatus(ctx, splitdb.UpdateSplitStatusParams{ID: id, Status: string(status)})
	return r.withRoles(ctx, "SetStatus", row, err)
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	if err := r.q.DeleteSplit(ctx, id); err != nil {
		return fmt.Errorf("split.repository.Delete: %w", err)
	}
	return nil
}

func (r *Repository) ListForUser(ctx context.Context, userID int64, memberIDs []int64, role *string, status *Status) ([]Split, error) {
	var st *string
	if status != nil {
		v := string(*status)
		st = &v
	}
	if memberIDs == nil {
		memberIDs = []int64{}
	}
	rows, err := r.q.ListSplitsForUser(ctx, splitdb.ListSplitsForUserParams{
		UserID: userID, MemberIds: memberIDs, Role: role, Status: st,
	})
	if err != nil {
		return nil, fmt.Errorf("split.repository.ListForUser: %w", err)
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	roleRows, err := r.q.ListRolesBySplitIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("split.repository.ListForUser: roles: %w", err)
	}
	byID := map[int64][]Role{}
	for _, rr := range roleRows {
		byID[rr.SplitID] = append(byID[rr.SplitID], toRole(rr))
	}
	out := make([]Split, len(rows))
	for i, row := range rows {
		out[i] = toSplit(row, byID[row.ID])
	}
	return out, nil
}

func (r *Repository) insertRoles(ctx context.Context, splitID int64, in []RoleInput) ([]Role, error) {
	roles := make([]Role, 0, len(in))
	for i, ri := range in {
		order := i
		if ri.SortOrder != nil {
			order = *ri.SortOrder
		}
		row, err := r.q.InsertRole(ctx, splitdb.InsertRoleParams{
			SplitID: splitID, Name: ri.Name,
			PerTeamMin: int32(ri.PerTeamMin), PerTeamMax: int32(ri.PerTeamMax),
			SortOrder: int32(order),
		})
		if err != nil {
			return nil, fmt.Errorf("insert role %q: %w", ri.Name, err)
		}
		roles = append(roles, toRole(row))
	}
	return roles, nil
}

func (r *Repository) withRoles(ctx context.Context, op string, row splitdb.Split, err error) (Split, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Split{}, ErrNotFound
	}
	if err != nil {
		return Split{}, fmt.Errorf("split.repository.%s: %w", op, err)
	}
	rows, err := r.q.ListRolesBySplitIDs(ctx, []int64{row.ID})
	if err != nil {
		return Split{}, fmt.Errorf("split.repository.%s: roles: %w", op, err)
	}
	roles := make([]Role, len(rows))
	for i, rr := range rows {
		roles[i] = toRole(rr)
	}
	return toSplit(row, roles), nil
}

func toSplit(row splitdb.Split, roles []Role) Split {
	var maxMembers *int
	if row.MaxMembers != nil {
		v := int(*row.MaxMembers)
		maxMembers = &v
	}
	return Split{
		ID: row.ID, OwnerID: row.OwnerID, Title: row.Title, Description: row.Description,
		Status: Status(row.Status), TeamSize: int(row.TeamSize), MaxMembers: maxMembers,
		RemainderPolicy: RemainderPolicy(row.RemainderPolicy), Roles: roles,
		AutoFinishAt: row.AutoFinishAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toRole(r splitdb.SplitRole) Role {
	return Role{
		ID: r.ID, Name: r.Name, PerTeamMin: int(r.PerTeamMin),
		PerTeamMax: int(r.PerTeamMax), SortOrder: int(r.SortOrder),
	}
}

func toInt32Ptr(v *int) *int32 {
	if v == nil {
		return nil
	}
	x := int32(*v)
	return &x
}

// PgTxRunner реализует TxRunner поверх пула pgx.
type PgTxRunner struct {
	pool *pgxpool.Pool
}

func NewPgTxRunner(pool *pgxpool.Pool) *PgTxRunner { return &PgTxRunner{pool: pool} }

func (p *PgTxRunner) Run(ctx context.Context, fn func(Store) error) (err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("split.PgTxRunner.Run: begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(NewRepository(tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("split.PgTxRunner.Run: commit tx: %w", err)
	}
	return nil
}
