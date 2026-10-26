package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	userdb "builder/backend/internal/user/sqlc/db"
)

const pgUniqueViolation = "23505"

type Repository struct {
	q *userdb.Queries
}

func NewRepository(db userdb.DBTX) *Repository {
	return &Repository{q: userdb.New(db)}
}

func (r *Repository) Create(ctx context.Context, a NewAccount) (Account, error) {
	row, err := r.q.CreateUser(ctx, userdb.CreateUserParams{
		Login:        a.Login,
		PasswordHash: a.PasswordHash,
		FirstName:    a.FirstName,
		MiddleName:   a.MiddleName,
		LastName:     a.LastName,
		Link:         a.Link,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return Account{}, ErrLoginTaken
	}
	if err != nil {
		return Account{}, fmt.Errorf("user.repository.Create: %w", err)
	}
	return toAccount(row), nil
}

func (r *Repository) GetByLogin(ctx context.Context, login string) (Account, error) {
	row, err := r.q.GetUserByLogin(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("user.repository.GetByLogin: %w", err)
	}
	return toAccount(row), nil
}

func (r *Repository) GetByID(ctx context.Context, id int64) (Account, error) {
	row, err := r.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("user.repository.GetByID: %w", err)
	}
	return toAccount(row), nil
}

func toAccount(u userdb.User) Account {
	return Account{
		User: User{
			ID:         u.ID,
			Login:      u.Login,
			FirstName:  u.FirstName,
			MiddleName: u.MiddleName,
			LastName:   u.LastName,
			Link:       u.Link,
			CreatedAt:  u.CreatedAt,
		},
		PasswordHash: u.PasswordHash,
	}
}

func (r *Repository) Update(ctx context.Context, id int64, u ProfileUpdate) (Account, error) {
	row, err := r.q.UpdateUser(ctx, userdb.UpdateUserParams{
		ID:            id,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		SetMiddleName: u.MiddleName.Set,
		MiddleName:    u.MiddleName.Value,
		SetLink:       u.Link.Set,
		Link:          u.Link.Value,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("user.repository.Update: %w", err)
	}
	return toAccount(row), nil
}

// ListByIDs возвращает найденных пользователей; отсутствующие id молча
// пропускаются, решать, что это ошибка, должен вызывающий.
func (r *Repository) ListByIDs(ctx context.Context, ids []int64) ([]User, error) {
	rows, err := r.q.ListUsersByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("user.repository.ListByIDs: %w", err)
	}
	out := make([]User, len(rows))
	for i, row := range rows {
		out[i] = toAccount(row).User
	}
	return out, nil
}
