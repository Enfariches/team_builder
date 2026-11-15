package team

import "context"

// SplitReader отдаёт состояние пространства без проверки доступа: права team
// определяет сам. Нет пространства — ErrSplitNotFound.
type SplitReader interface {
	State(ctx context.Context, splitID int64) (SplitState, error)
}

// MemberDirectory — участники, которыми владеет модуль member.
type MemberDirectory interface {
	ListAssignments(ctx context.Context, splitID int64) ([]Assignment, error)
	IsMember(ctx context.Context, splitID, userID int64) (bool, error)
}

type UserDirectory interface {
	ListBriefs(ctx context.Context, ids []int64) ([]UserBrief, error)
}

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста
// запроса. Реализацию (из модуля auth) подставляет cmd/api/main.go.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
