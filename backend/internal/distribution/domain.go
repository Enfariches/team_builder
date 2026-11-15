package distribution

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNotEnoughMembers — участников меньше, чем нужно для одной команды.
	ErrNotEnoughMembers = errors.New("not enough members")
	// ErrInfeasible — состав нельзя разбить, не нарушив квоты ролей.
	ErrInfeasible = errors.New("distribution is infeasible")
	// ErrRosterChanged — состав или правила изменились после пробного прогона.
	ErrRosterChanged = errors.New("roster changed since the run")
	// ErrAlreadyFinished — распределение уже опубликовано (для preview).
	ErrAlreadyFinished = errors.New("distribution already finished")
	// ErrSplitNotOpen — набор ещё не открывался (статус Draft).
	ErrSplitNotOpen = errors.New("split is not open")
	// ErrRunNotFound — прогона нет в этом пространстве.
	ErrRunNotFound = errors.New("run not found")
	// ErrSplitNotFound — пространства нет или у пользователя нет к нему доступа.
	ErrSplitNotFound = errors.New("split not found")
	// ErrForbidden — пользователь связан с пространством, но не владелец.
	ErrForbidden = errors.New("owner only")
)

// Algorithm — единственный сейчас алгоритм; значение хранится в прогоне.
const AlgorithmGreedyV1 = "greedy_v1"

type RemainderPolicy string

const (
	PolicySmallerLastTeam RemainderPolicy = "smaller_last_team"
	PolicySpread          RemainderPolicy = "spread"
	PolicyWaitlist        RemainderPolicy = "waitlist"
)

// Статусы пространства, значимые для распределения. Значения совпадают с
// контрактом API; импортировать split ради них нельзя.
const (
	StatusDraft    = "Draft"
	StatusFinished = "Finished"
)

type Role struct {
	Name      string
	Min, Max  int
	SortOrder int
}

// Rules — правила состава пространства. Предполагаются корректными:
// sum(Min) <= TeamSize <= sum(Max), Min <= Max (это проверяет модуль split).
type Rules struct {
	TeamSize int
	Roles    []Role
	Policy   RemainderPolicy
}

type RolePreference struct {
	Role string
	Rank int
}

// Candidate — участник, как его видит распределение.
type Candidate struct {
	MemberID    int64
	UserID      int64
	JoinedAt    time.Time
	Experience  int
	Preferences []RolePreference
}

type Assignment struct {
	MemberID int64
	Role     string
}

type Team struct {
	Name    string
	Members []Assignment
}

// Result — состав команд и лист ожидания (id участников).
type Result struct {
	Teams    []Team
	Waitlist []int64
}

type Metrics struct {
	TeamsCount           int     `json:"teamsCount"`
	MembersAssigned      int     `json:"membersAssigned"`
	MembersWaitlisted    int     `json:"membersWaitlisted"`
	RoleQuotasSatisfied  bool    `json:"roleQuotasSatisfied"`
	FirstChoiceRoleShare float64 `json:"firstChoiceRoleShare"`
	ExperienceSpread     float64 `json:"experienceSpread"`
}

type Problem struct {
	Field   string
	Message string
}

// InfeasibleError несёт перечень причин. Он не FieldsError: ответ — 409, а не
// 422, поэтому причины отдаются через platform.DetailsError.
type InfeasibleError struct {
	Problems []Problem
}

func (e *InfeasibleError) Error() string { return "distribution is infeasible" }
func (e *InfeasibleError) Unwrap() error { return ErrInfeasible }

func (e *InfeasibleError) Details() [][2]string {
	out := make([][2]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = [2]string{p.Field, p.Message}
	}
	return out
}

func infeasible(format string, args ...any) error {
	return &InfeasibleError{Problems: []Problem{{Field: "remainderPolicy", Message: fmt.Sprintf(format, args...)}}}
}

// Run — сохранённый прогон. Result неизменяем после создания.
type Run struct {
	ID          int64
	SplitID     int64
	Algorithm   string
	Seed        int64
	Roster      []int64
	Result      Result
	Metrics     Metrics
	TeamIDs     []int64 // id созданных команд в порядке Result.Teams; пусто до публикации
	CreatedAt   time.Time
	PublishedAt *time.Time
}

func (r Run) Published() bool { return r.PublishedAt != nil }

// SplitState — то, что распределение знает о пространстве.
type SplitState struct {
	OwnerID int64
	Status  string
	Rules   Rules
}
