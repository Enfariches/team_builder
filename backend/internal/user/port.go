package user

import "context"

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста
// запроса. Реализацию (из модуля auth) подставляет cmd/api/main.go.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
