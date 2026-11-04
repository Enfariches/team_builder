package invite

import "context"

// SplitAccess отдаёт роль пользователя в пространстве. Для постороннего и для
// несуществующего пространства возвращает ErrSplitNotFound.
type SplitAccess interface {
	Authorize(ctx context.Context, userID, splitID int64) (Access, error)
}

// SplitInfoProvider отдаёт публичные сведения без проверки доступа: ими
// пользуется предпросмотр, где вызывающий владеет только токеном.
type SplitInfoProvider interface {
	SplitInfo(ctx context.Context, splitID int64) (SplitInfo, error)
}

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста
// запроса. Реализацию (из модуля auth) подставляет cmd/api/main.go.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
