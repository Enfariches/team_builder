package member

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	memberdb "builder/backend/internal/member/sqlc/db"
)

const uniqueViolation = "23505"

type Repository struct {
	q *memberdb.Queries
}

func NewRepository(db memberdb.DBTX) *Repository {
	return &Repository{q: memberdb.New(db)}
}

// LockSplit берёт advisory-lock до конца транзакции; вне транзакции он
// отпустится сразу же, поэтому вызывать только внутри PgTxRunner.Run.
func (r *Repository) LockSplit(ctx context.Context, splitID int64) error {
	if err := r.q.LockSplit(ctx, splitID); err != nil {
		return fmt.Errorf("member.repository.LockSplit: %w", err)
	}
	return nil
}

func (r *Repository) Insert(ctx context.Context, n NewMember) (Member, error) {
	raw, err := json.Marshal(snapshotToJSON(n.Snapshot))
	if err != nil {
		return Member{}, fmt.Errorf("member.repository.Insert: marshal snapshot: %w", err)
	}
	formID := n.FormID
	row, err := r.q.InsertMember(ctx, memberdb.InsertMemberParams{
		SplitID: n.SplitID, UserID: n.UserID, FormID: &formID, FormSnapshot: raw,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return Member{}, ErrAlreadyMember
	}
	if err != nil {
		return Member{}, fmt.Errorf("member.repository.Insert: %w", err)
	}
	return toMember(row)
}

func (r *Repository) Get(ctx context.Context, id int64) (Member, error) {
	row, err := r.q.GetMember(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("member.repository.Get: %w", err)
	}
	return toMember(row)
}

func (r *Repository) GetByUser(ctx context.Context, splitID, userID int64) (Member, error) {
	row, err := r.q.GetMemberByUser(ctx, memberdb.GetMemberByUserParams{SplitID: splitID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("member.repository.GetByUser: %w", err)
	}
	return toMember(row)
}

func (r *Repository) List(ctx context.Context, splitID int64) ([]Member, error) {
	rows, err := r.q.ListMembersBySplit(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("member.repository.List: %w", err)
	}
	out := make([]Member, len(rows))
	for i, row := range rows {
		if out[i], err = toMember(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Repository) Count(ctx context.Context, splitID int64) (int, error) {
	n, err := r.q.CountMembersBySplit(ctx, splitID)
	if err != nil {
		return 0, fmt.Errorf("member.repository.Count: %w", err)
	}
	return int(n), nil
}

func (r *Repository) Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error) {
	if splitIDs == nil {
		splitIDs = []int64{}
	}
	rows, err := r.q.CountMembersBySplits(ctx, splitIDs)
	if err != nil {
		return nil, fmt.Errorf("member.repository.Counts: %w", err)
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.SplitID] = int(row.MembersCount)
	}
	return out, nil
}

func (r *Repository) SplitIDsOfUser(ctx context.Context, userID int64) ([]int64, error) {
	ids, err := r.q.SplitIDsOfUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("member.repository.SplitIDsOfUser: %w", err)
	}
	return ids, nil
}

func (r *Repository) IsMember(ctx context.Context, splitID, userID int64) (bool, error) {
	ok, err := r.q.IsMember(ctx, memberdb.IsMemberParams{SplitID: splitID, UserID: userID})
	if err != nil {
		return false, fmt.Errorf("member.repository.IsMember: %w", err)
	}
	return ok, nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	n, err := r.q.DeleteMember(ctx, id)
	if err != nil {
		return fmt.Errorf("member.repository.Delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// snapshotJSON — формат form_snapshot в БД. Отделён от доменного типа, чтобы
// смена Go-структуры не меняла сохранённые данные.
type snapshotJSON struct {
	Experience      int                  `json:"experience"`
	Skills          []string             `json:"skills"`
	RolePreferences []rolePreferenceJSON `json:"rolePreferences"`
	Comment         *string              `json:"comment"`
}

type rolePreferenceJSON struct {
	RoleName string `json:"roleName"`
	Rank     int    `json:"rank"`
}

func snapshotToJSON(s FormSnapshot) snapshotJSON {
	prefs := make([]rolePreferenceJSON, len(s.RolePreferences))
	for i, p := range s.RolePreferences {
		prefs[i] = rolePreferenceJSON{RoleName: p.RoleName, Rank: p.Rank}
	}
	skills := s.Skills
	if skills == nil {
		skills = []string{}
	}
	return snapshotJSON{Experience: s.Experience, Skills: skills, RolePreferences: prefs, Comment: s.Comment}
}

func toMember(row memberdb.Member) (Member, error) {
	var js snapshotJSON
	if err := json.Unmarshal(row.FormSnapshot, &js); err != nil {
		return Member{}, fmt.Errorf("member.repository: unmarshal snapshot of member %d: %w", row.ID, err)
	}
	prefs := make([]RolePreference, len(js.RolePreferences))
	for i, p := range js.RolePreferences {
		prefs[i] = RolePreference{RoleName: p.RoleName, Rank: p.Rank}
	}
	return Member{
		ID: row.ID, SplitID: row.SplitID, UserID: row.UserID, FormID: row.FormID,
		Snapshot: FormSnapshot{
			Experience: js.Experience, Skills: js.Skills, RolePreferences: prefs, Comment: js.Comment,
		},
		TeamID: row.TeamID, AssignedRole: row.AssignedRole, JoinedAt: row.CreatedAt,
	}, nil
}

// PgTxRunner реализует TxRunner поверх пула pgx.
type PgTxRunner struct {
	pool *pgxpool.Pool
}

func NewPgTxRunner(pool *pgxpool.Pool) *PgTxRunner { return &PgTxRunner{pool: pool} }

func (p *PgTxRunner) Run(ctx context.Context, fn func(Store) error) (err error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("member.PgTxRunner.Run: begin tx: %w", err)
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
		return fmt.Errorf("member.PgTxRunner.Run: commit tx: %w", err)
	}
	return nil
}
