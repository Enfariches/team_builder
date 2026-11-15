package team

import (
	"context"
	"fmt"

	"builder/backend/internal/platform"
	teamdb "builder/backend/internal/team/sqlc/db"
)

type Repository struct {
	db teamdb.DBTX
}

func NewRepository(db teamdb.DBTX) *Repository { return &Repository{db: db} }

// q берёт транзакцию из ctx (общая транзакция публикации), а если её нет —
// работает на соединении, с которым создан репозиторий.
func (r *Repository) q(ctx context.Context) *teamdb.Queries {
	return teamdb.New(platform.DB(ctx, r.db))
}

func (r *Repository) Create(ctx context.Context, splitID int64, names []string) ([]int64, error) {
	ids := make([]int64, len(names))
	for i, name := range names {
		row, err := r.q(ctx).InsertTeam(ctx, teamdb.InsertTeamParams{SplitID: splitID, Name: name, SortOrder: int32(i)})
		if err != nil {
			return nil, fmt.Errorf("team.repository.Create: %w", err)
		}
		ids[i] = row.ID
	}
	return ids, nil
}

func (r *Repository) List(ctx context.Context, splitID int64) ([]Team, error) {
	rows, err := r.q(ctx).ListTeamsBySplit(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("team.repository.List: %w", err)
	}
	out := make([]Team, len(rows))
	for i, row := range rows {
		out[i] = Team{ID: row.ID, SplitID: row.SplitID, Name: row.Name, CreatedAt: row.CreatedAt}
	}
	return out, nil
}

func (r *Repository) Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error) {
	if splitIDs == nil {
		splitIDs = []int64{}
	}
	rows, err := r.q(ctx).CountTeamsBySplits(ctx, splitIDs)
	if err != nil {
		return nil, fmt.Errorf("team.repository.Counts: %w", err)
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.SplitID] = int(row.TeamsCount)
	}
	return out, nil
}
