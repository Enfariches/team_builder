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
