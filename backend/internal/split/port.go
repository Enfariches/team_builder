package split

import "context"

// MembershipProvider — сведения об участниках, которыми владеет модуль member.
type MembershipProvider interface {
	IsMember(ctx context.Context, splitID, userID int64) (bool, error)
	SplitIDsOfUser(ctx context.Context, userID int64) ([]int64, error)
}

// StatsProvider отдаёт счётчики, принадлежащие другим модулям. В результате
// допустимо отсутствие ключа: это то же, что нулевая статистика.
type StatsProvider interface {
	Stats(ctx context.Context, splitIDs []int64) (map[int64]Stats, error)
}

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
