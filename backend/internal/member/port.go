package member

import "context"

// InviteRedeemer занимает и возвращает использование инвайта. Ошибку
// неверного токена реализация приводит к ErrInviteInvalid.
type InviteRedeemer interface {
	Redeem(ctx context.Context, token string) (Redemption, error)
	Release(ctx context.Context, inviteID int64) error
}

// FormSnapshotter отдаёт содержимое анкеты пользователя. Чужая или
// несуществующая анкета — ErrFormNotFound.
type FormSnapshotter interface {
	Snapshot(ctx context.Context, userID, formID int64) (FormSnapshot, error)
}

// SplitReader отдаёт сведения о пространстве без проверки доступа: права
// member определяет сам. Нет пространства — ErrSplitNotFound.
type SplitReader interface {
	SplitInfo(ctx context.Context, splitID int64) (SplitInfo, error)
}

// UserDirectory отдаёт публичные сведения о пользователях.
type UserDirectory interface {
	ListBriefs(ctx context.Context, ids []int64) ([]UserBrief, error)
}

// EventPublisher сообщает о смене состава лобби (событие SSE member_joined).
type EventPublisher interface {
	MembersChanged(splitID int64, membersCount int)
}

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста
// запроса. Реализацию (из модуля auth) подставляет cmd/api/main.go.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
