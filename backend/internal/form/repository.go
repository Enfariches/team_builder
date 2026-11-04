package form

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	formdb "builder/backend/internal/form/sqlc/db"
)

type Repository struct {
	q *formdb.Queries
}

func NewRepository(db formdb.DBTX) *Repository {
	return &Repository{q: formdb.New(db)}
}

type prefJSON struct {
	RoleName string `json:"roleName"`
	Rank     int    `json:"rank"`
}

func marshalPrefs(ps []RolePreference) ([]byte, error) {
	out := make([]prefJSON, len(ps))
	for i, p := range ps {
		out[i] = prefJSON{p.RoleName, p.Rank}
	}
	return json.Marshal(out)
}

func (r *Repository) Create(ctx context.Context, userID int64, n NewForm) (Form, error) {
	prefs, err := marshalPrefs(n.RolePreferences)
	if err != nil {
		return Form{}, fmt.Errorf("form.repository.Create: marshal preferences: %w", err)
	}
	row, err := r.q.InsertForm(ctx, formdb.InsertFormParams{
		UserID:          userID,
		Name:            n.Name,
		Experience:      int16(n.Experience),
		Skills:          nonNil(n.Skills),
		RolePreferences: prefs,
		Comment:         n.Comment,
	})
	if err != nil {
		return Form{}, fmt.Errorf("form.repository.Create: %w", err)
	}
	return toForm(row)
}

func (r *Repository) Get(ctx context.Context, userID, id int64) (Form, error) {
	row, err := r.q.GetForm(ctx, formdb.GetFormParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Form{}, ErrNotFound
	}
	if err != nil {
		return Form{}, fmt.Errorf("form.repository.Get: %w", err)
	}
	return toForm(row)
}

func (r *Repository) List(ctx context.Context, userID int64) ([]Form, error) {
	rows, err := r.q.ListFormsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("form.repository.List: %w", err)
	}
	out := make([]Form, len(rows))
	for i, row := range rows {
		if out[i], err = toForm(row); err != nil {
			return nil, fmt.Errorf("form.repository.List: %w", err)
		}
	}
	return out, nil
}

func (r *Repository) Update(ctx context.Context, userID, id int64, u Update) (Form, error) {
	p := formdb.UpdateFormParams{
		ID:         id,
		UserID:     userID,
		Name:       u.Name,
		SetComment: u.Comment.Set,
		Comment:    u.Comment.Value,
	}
	if u.Experience != nil {
		e := int16(*u.Experience)
		p.Experience = &e
	}
	if u.Skills != nil {
		p.Skills = nonNil(*u.Skills)
	}
	if u.RolePreferences != nil {
		b, err := marshalPrefs(*u.RolePreferences)
		if err != nil {
			return Form{}, fmt.Errorf("form.repository.Update: marshal preferences: %w", err)
		}
		p.RolePreferences = b
	}
	row, err := r.q.UpdateForm(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return Form{}, ErrNotFound
	}
	if err != nil {
		return Form{}, fmt.Errorf("form.repository.Update: %w", err)
	}
	return toForm(row)
}

func (r *Repository) Delete(ctx context.Context, userID, id int64) error {
	n, err := r.q.DeleteForm(ctx, formdb.DeleteFormParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("form.repository.Delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// nonNil: пустой срез должен уйти в БД как '{}', а не как NULL, иначе
// COALESCE в UpdateForm не сможет отличить «очистить» от «не менять».
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toForm(row formdb.Form) (Form, error) {
	var prefs []prefJSON
	if err := json.Unmarshal(row.RolePreferences, &prefs); err != nil {
		return Form{}, fmt.Errorf("form.repository: unmarshal preferences of form %d: %w", row.ID, err)
	}
	out := make([]RolePreference, len(prefs))
	for i, p := range prefs {
		out[i] = RolePreference{p.RoleName, p.Rank}
	}
	skills := row.Skills
	if skills == nil {
		skills = []string{}
	}
	return Form{
		ID:              row.ID,
		UserID:          row.UserID,
		Name:            row.Name,
		Experience:      int(row.Experience),
		Skills:          skills,
		RolePreferences: out,
		Comment:         row.Comment,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}
