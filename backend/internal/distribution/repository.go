package distribution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	distributiondb "builder/backend/internal/distribution/sqlc/db"
	"builder/backend/internal/platform"
)

type Repository struct {
	db distributiondb.DBTX
}

func NewRepository(db distributiondb.DBTX) *Repository { return &Repository{db: db} }

// q берёт транзакцию из ctx (общая транзакция публикации), а если её нет —
// работает на соединении, с которым создан репозиторий.
func (r *Repository) q(ctx context.Context) *distributiondb.Queries {
	return distributiondb.New(platform.DB(ctx, r.db))
}

func (r *Repository) InsertRun(ctx context.Context, run Run) (Run, error) {
	result, err := json.Marshal(resultToJSON(run.Result))
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.InsertRun: marshal result: %w", err)
	}
	metrics, err := json.Marshal(run.Metrics)
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.InsertRun: marshal metrics: %w", err)
	}
	row, err := r.q(ctx).InsertRun(ctx, distributiondb.InsertRunParams{
		SplitID: run.SplitID, Algorithm: run.Algorithm, Seed: run.Seed,
		Roster: run.Roster, Result: result, Metrics: metrics,
	})
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.InsertRun: %w", err)
	}
	return toRun(row)
}

func (r *Repository) GetRun(ctx context.Context, id, splitID int64) (Run, error) {
	row, err := r.q(ctx).GetRun(ctx, distributiondb.GetRunParams{ID: id, SplitID: splitID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.GetRun: %w", err)
	}
	return toRun(row)
}

func (r *Repository) GetPublishedRun(ctx context.Context, splitID int64) (Run, error) {
	row, err := r.q(ctx).GetPublishedRun(ctx, splitID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.GetPublishedRun: %w", err)
	}
	return toRun(row)
}

func (r *Repository) ListRuns(ctx context.Context, splitID int64) ([]Run, error) {
	rows, err := r.q(ctx).ListRunsBySplit(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("distribution.repository.ListRuns: %w", err)
	}
	out := make([]Run, len(rows))
	for i, row := range rows {
		if out[i], err = toRun(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MarkPublished не находит строку, если прогон уже опубликован: это
// нарушение идемпотентности, а не штатная ситуация, поэтому ошибка не доменная.
func (r *Repository) MarkPublished(ctx context.Context, id int64, teamIDs []int64) (Run, error) {
	row, err := r.q(ctx).MarkRunPublished(ctx, distributiondb.MarkRunPublishedParams{ID: id, TeamIds: teamIDs})
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("distribution.repository.MarkPublished: run %d is already published", id)
	}
	if err != nil {
		return Run{}, fmt.Errorf("distribution.repository.MarkPublished: %w", err)
	}
	return toRun(row)
}

// Формат result в БД отделён от доменных типов: смена Go-структур не должна
// менять сохранённые данные.
type resultJSON struct {
	Teams    []teamJSON `json:"teams"`
	Waitlist []int64    `json:"waitlist"`
}

type teamJSON struct {
	Name    string       `json:"name"`
	Members []memberJSON `json:"members"`
}

type memberJSON struct {
	MemberID int64  `json:"memberId"`
	Role     string `json:"role"`
}

func resultToJSON(r Result) resultJSON {
	out := resultJSON{Teams: make([]teamJSON, len(r.Teams)), Waitlist: r.Waitlist}
	if out.Waitlist == nil {
		out.Waitlist = []int64{}
	}
	for i, t := range r.Teams {
		tj := teamJSON{Name: t.Name, Members: make([]memberJSON, len(t.Members))}
		for j, m := range t.Members {
			tj.Members[j] = memberJSON{MemberID: m.MemberID, Role: m.Role}
		}
		out.Teams[i] = tj
	}
	return out
}

func toRun(row distributiondb.DistributionRun) (Run, error) {
	var rj resultJSON
	if err := json.Unmarshal(row.Result, &rj); err != nil {
		return Run{}, fmt.Errorf("distribution.repository: unmarshal result of run %d: %w", row.ID, err)
	}
	var metrics Metrics
	if err := json.Unmarshal(row.Metrics, &metrics); err != nil {
		return Run{}, fmt.Errorf("distribution.repository: unmarshal metrics of run %d: %w", row.ID, err)
	}
	res := Result{Teams: make([]Team, len(rj.Teams))}
	for i, t := range rj.Teams {
		team := Team{Name: t.Name, Members: make([]Assignment, len(t.Members))}
		for j, m := range t.Members {
			team.Members[j] = Assignment{MemberID: m.MemberID, Role: m.Role}
		}
		res.Teams[i] = team
	}
	if len(rj.Waitlist) > 0 {
		res.Waitlist = rj.Waitlist
	}
	return Run{
		ID: row.ID, SplitID: row.SplitID, Algorithm: row.Algorithm, Seed: row.Seed,
		Roster: row.Roster, Result: res, Metrics: metrics, TeamIDs: row.TeamIds,
		CreatedAt: row.CreatedAt, PublishedAt: row.PublishedAt,
	}, nil
}
