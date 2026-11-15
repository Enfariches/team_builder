package distribution

import "context"

// SplitGate — всё, что распределению нужно от модуля split. Методы с lock и
// запись работают в транзакции из ctx (platform.TxManager).
type SplitGate interface {
	// State читает пространство без блокировки. Нет пространства — ErrSplitNotFound.
	State(ctx context.Context, splitID int64) (SplitState, error)
	// LockForPublish блокирует строку пространства (SELECT ... FOR UPDATE) и
	// отдаёт актуальное состояние.
	LockForPublish(ctx context.Context, splitID int64) (SplitState, error)
	// MarkFinished переводит Open/Closed в Finished в обход ручных переходов.
	MarkFinished(ctx context.Context, splitID int64) error
}

// Placement — куда и в какой роли попал участник.
type Placement struct {
	MemberID int64
	TeamID   int64
	Role     string
}

// Roster — участники пространства, которыми владеет модуль member.
type Roster interface {
	// Lock берёт advisory-lock пространства до конца транзакции: тот же, что
	// держит member при вступлении и выходе.
	Lock(ctx context.Context, splitID int64) error
	Candidates(ctx context.Context, splitID int64) ([]Candidate, error)
	IsMember(ctx context.Context, splitID, userID int64) (bool, error)
	Assign(ctx context.Context, splitID int64, placements []Placement) error
}

// TeamWriter создаёт команды в таблице модуля team; ids идут в порядке names.
type TeamWriter interface {
	CreateTeams(ctx context.Context, splitID int64, names []string) ([]int64, error)
}

type UserDirectory interface {
	ListBriefs(ctx context.Context, ids []int64) ([]UserBrief, error)
}

// EventPublisher сообщает подписчикам потока пространства об успешной публикации.
type EventPublisher interface {
	Published(splitID, runID int64)
}

// TxManager выполняет fn в транзакции, общей для всех модулей, чьи методы
// вызываются с переданным ctx.
type TxManager interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}

// IdentityProvider отдаёт id аутентифицированного пользователя из контекста.
type IdentityProvider interface {
	UserID(ctx context.Context) (int64, bool)
}
