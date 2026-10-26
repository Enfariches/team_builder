package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/api/gen"
	"builder/backend/internal/auth"
	"builder/backend/internal/split"
	"builder/backend/internal/user"
)

// notImplemented закрывает операции, чьи модули ещё не написаны. Вызов такой
// операции упадёт в nil-интерфейсе, Recover вернёт 500.
type notImplemented struct{ gen.StrictServerInterface }

// Алиасы нужны, чтобы у встроенных обработчиков были разные имена полей:
// оба типа называются Handler.
type (
	authAPI  = auth.Handler
	userAPI  = user.Handler
	splitAPI = split.Handler
)

type apiServer struct {
	notImplemented
	*authAPI
	*userAPI
	*splitAPI
	pool *pgxpool.Pool
}

func (s *apiServer) GetHealth(ctx context.Context, _ gen.GetHealthRequestObject) (gen.GetHealthResponseObject, error) {
	status, db := gen.Ok, gen.Up
	if err := s.pool.Ping(ctx); err != nil {
		status, db = gen.Degraded, gen.Down
	}
	h := gen.Health{Status: &status, Database: &db}
	if db == gen.Down {
		return gen.GetHealth503JSONResponse{ServiceUnavailableJSONResponse: gen.ServiceUnavailableJSONResponse{
			Code: "service_unavailable", Message: "База данных недоступна",
		}}, nil
	}
	return gen.GetHealth200JSONResponse(h), nil
}

// publicOperations — операции со `security: []`: токен для них не нужен.
func publicOperations(swagger *openapi3.T) map[string]bool {
	public := map[string]bool{}
	for _, item := range swagger.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security != nil && len(*op.Security) == 0 {
				public[op.OperationID] = true
			}
		}
	}
	return public
}

// userStoreAdapter переводит типы модуля user в типы, которые объявил auth.
type userStoreAdapter struct{ svc *user.Service }

func (a userStoreAdapter) Create(ctx context.Context, u auth.NewUser) (auth.User, error) {
	acc, err := a.svc.Create(ctx, user.NewAccount{
		Login: u.Login, PasswordHash: u.PasswordHash,
		FirstName: u.FirstName, MiddleName: u.MiddleName,
		LastName: u.LastName, Link: u.Link,
	})
	if err != nil {
		return auth.User{}, mapUserErr("Create", err)
	}
	return toAuthUser(acc.User), nil
}

func (a userStoreAdapter) GetByLogin(ctx context.Context, login string) (auth.UserWithHash, error) {
	acc, err := a.svc.GetByLogin(ctx, login)
	if err != nil {
		return auth.UserWithHash{}, mapUserErr("GetByLogin", err)
	}
	return auth.UserWithHash{User: toAuthUser(acc.User), PasswordHash: acc.PasswordHash}, nil
}

func (a userStoreAdapter) GetByID(ctx context.Context, id int64) (auth.User, error) {
	acc, err := a.svc.GetByID(ctx, id)
	if err != nil {
		return auth.User{}, mapUserErr("GetByID", err)
	}
	return toAuthUser(acc.User), nil
}

func toAuthUser(u user.User) auth.User {
	return auth.User{
		ID: u.ID, Login: u.Login, FirstName: u.FirstName, MiddleName: u.MiddleName,
		LastName: u.LastName, Link: u.Link, CreatedAt: u.CreatedAt,
	}
}

// mapUserErr переводит ошибки user в ошибки auth, сохраняя исходную цепочку.
func mapUserErr(op string, err error) error {
	switch {
	case errors.Is(err, user.ErrLoginTaken):
		return fmt.Errorf("userStoreAdapter.%s: %w (%w)", op, auth.ErrLoginTaken, err)
	case errors.Is(err, user.ErrNotFound):
		return fmt.Errorf("userStoreAdapter.%s: %w (%w)", op, auth.ErrUserNotFound, err)
	default:
		return fmt.Errorf("userStoreAdapter.%s: %w", op, err)
	}
}

// memberStub — временная реализация портов split, пока нет модуля member:
// участников нет, поэтому доступ есть только у владельцев.
// TODO(member): заменить реализацией из модуля member.
type memberStub struct{}

func (memberStub) IsMember(context.Context, int64, int64) (bool, error) { return false, nil }

func (memberStub) SplitIDsOfUser(context.Context, int64) ([]int64, error) { return nil, nil }

func (memberStub) Stats(context.Context, []int64) (map[int64]split.Stats, error) {
	return map[int64]split.Stats{}, nil
}
