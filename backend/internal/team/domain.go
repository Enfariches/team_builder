package team

import (
	"errors"
	"time"
)

var (
	// ErrSplitNotFound — пространства нет или у пользователя нет к нему доступа.
	ErrSplitNotFound = errors.New("split not found")
	// ErrForbidden — пользователь связан с пространством, но не владелец.
	ErrForbidden = errors.New("owner only")
	// ErrNotFinished — распределение ещё не опубликовано.
	ErrNotFinished = errors.New("distribution is not published")
	// ErrNotAssigned — пользователь не попал в команду (лист ожидания).
	ErrNotAssigned = errors.New("user is not assigned to a team")
)

// StatusFinished совпадает со статусом из контракта API; импортировать split
// ради константы нельзя.
const StatusFinished = "Finished"

type UserBrief struct {
	ID        int64
	FirstName string
	LastName  string
	Link      *string
}

type Member struct {
	MemberID int64
	User     UserBrief
	Role     *string
}

type Team struct {
	ID        int64
	SplitID   int64
	Name      string
	Members   []Member
	CreatedAt time.Time
}

// Assignment — размещение участника, как его отдаёт модуль member. Команды
// у участника может не быть (лист ожидания).
type Assignment struct {
	MemberID int64
	UserID   int64
	TeamID   *int64
	Role     *string
}

// SplitState — то, что team знает о пространстве.
type SplitState struct {
	OwnerID int64
	Status  string
}

// Waitlisted — участник без команды после публикации.
type Waitlisted struct {
	MemberID int64
	User     UserBrief
}

// Roster — опубликованный результат целиком: команды и лист ожидания.
type Roster struct {
	Teams    []Team
	Waitlist []Waitlisted
}
