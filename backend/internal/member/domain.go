package member

import (
	"errors"
	"time"
)

var (
	// ErrNotFound — участника нет или он не виден вызывающему.
	ErrNotFound = errors.New("member not found")
	// ErrSplitNotFound — пространства нет или у пользователя нет к нему доступа.
	ErrSplitNotFound = errors.New("split not found")
	// ErrForbidden — пользователь связан с пространством, но не владелец.
	ErrForbidden = errors.New("owner only")
	// ErrAlreadyMember — повторное вступление; владелец тоже считается участвующим.
	ErrAlreadyMember = errors.New("already a member")
	ErrSplitFull     = errors.New("split is full")
	ErrSplitNotOpen  = errors.New("split is not open")
	// ErrSplitFinished — распределение опубликовано, состав менять нельзя.
	ErrSplitFinished = errors.New("split is finished")
	// ErrInviteInvalid — токен неизвестен, отозван, истёк или исчерпан.
	ErrInviteInvalid = errors.New("invite is not valid")
	// ErrFormNotFound — анкеты нет или она чужая; наличие чужой не раскрываем.
	ErrFormNotFound = errors.New("form not found")
)

// Статусы пространства, значимые для участников. Значения совпадают с
// контрактом API; импортировать split ради них нельзя.
const (
	StatusOpen     = "Open"
	StatusFinished = "Finished"
)

type RolePreference struct {
	RoleName string
	Rank     int
}

// FormSnapshot — анкета, замороженная при вступлении. Правка исходной анкеты
// на распределение не влияет.
type FormSnapshot struct {
	Experience      int
	Skills          []string
	RolePreferences []RolePreference
	Comment         *string
}

type UserBrief struct {
	ID        int64
	FirstName string
	LastName  string
	Link      *string
}

type Member struct {
	ID           int64
	SplitID      int64
	UserID       int64
	User         UserBrief
	FormID       *int64
	Snapshot     FormSnapshot
	TeamID       *int64
	AssignedRole *string
	JoinedAt     time.Time
}

type NewMember struct {
	SplitID  int64
	UserID   int64
	FormID   int64
	Snapshot FormSnapshot
}

// SplitInfo — то, что member знает о пространстве.
type SplitInfo struct {
	OwnerID    int64
	Status     string
	MaxMembers *int
}

// Redemption — результат занятия использования инвайта.
type Redemption struct {
	InviteID int64
	SplitID  int64
}

// CheckCanJoin проверяет условия вступления. Порядок важен для клиента:
// сначала «ты уже тут», потом «набор закрыт», потом «мест нет».
func CheckCanJoin(info SplitInfo, userID int64, alreadyMember bool, count int) error {
	if alreadyMember || info.OwnerID == userID {
		return ErrAlreadyMember
	}
	if info.Status != StatusOpen {
		return ErrSplitNotOpen
	}
	if info.MaxMembers != nil && count >= *info.MaxMembers {
		return ErrSplitFull
	}
	return nil
}
