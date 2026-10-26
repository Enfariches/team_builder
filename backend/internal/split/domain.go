package split

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	titleMaxLen = 255
	// предел int32 в БД; без проверки огромное число из JSON дало бы 500
	maxInt32 = 1<<31 - 1
)

type Status string

const (
	StatusDraft    Status = "Draft"
	StatusOpen     Status = "Open"
	StatusClosed   Status = "Closed"
	StatusFinished Status = "Finished"
)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusOpen, StatusClosed, StatusFinished:
		return true
	}
	return false
}

// CanTransition описывает ручные переходы. Finished сюда не входит намеренно:
// в него пространство попадает только через публикацию распределения.
func (s Status) CanTransition(to Status) bool {
	switch s {
	case StatusDraft:
		return to == StatusOpen
	case StatusOpen:
		return to == StatusClosed
	case StatusClosed:
		return to == StatusOpen
	}
	return false
}

type RemainderPolicy string

const (
	PolicySmallerLastTeam RemainderPolicy = "smaller_last_team"
	PolicySpread          RemainderPolicy = "spread"
	PolicyWaitlist        RemainderPolicy = "waitlist"
)

func (p RemainderPolicy) Valid() bool {
	switch p {
	case PolicySmallerLastTeam, PolicySpread, PolicyWaitlist:
		return true
	}
	return false
}

// Access — роль пользователя в конкретном пространстве.
type Access string

const (
	AccessOwner  Access = "Owner"
	AccessMember Access = "Member"
)

type Role struct {
	ID         int64
	Name       string
	PerTeamMin int
	PerTeamMax int
	SortOrder  int
}

type Split struct {
	ID              int64
	OwnerID         int64
	Title           string
	Description     *string
	Status          Status
	TeamSize        int
	MaxMembers      *int
	RemainderPolicy RemainderPolicy
	Roles           []Role
	AutoFinishAt    *time.Time
	MembersCount    int
	TeamsCount      *int
	MyRole          Access
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Stats — данные, которыми владеют другие модули (участники, команды).
type Stats struct {
	MembersCount int
	TeamsCount   *int
}

type RoleInput struct {
	Name       string
	PerTeamMin int
	PerTeamMax int
	SortOrder  *int
}

type NewSplit struct {
	OwnerID         int64
	Title           string
	Description     *string
	TeamSize        int
	MaxMembers      *int
	RemainderPolicy RemainderPolicy
	Roles           []RoleInput
	AutoFinishAt    *time.Time
}

// Optional различает «поле не передано» (Set=false) и «передано»; Value=nil
// при Set=true означает явный null.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// Patch — частичное обновление пространства; nil/Set=false значит «не менять».
type Patch struct {
	Title           *string
	Description     Optional[string]
	TeamSize        *int
	MaxMembers      Optional[int]
	RemainderPolicy *RemainderPolicy
	Roles           *[]RoleInput
	AutoFinishAt    Optional[time.Time]
}

var (
	ErrNotFound                = errors.New("split not found")
	ErrForbidden               = errors.New("owner only")
	ErrRulesLocked             = errors.New("team rules are locked after draft")
	ErrSplitFinished           = errors.New("split is finished")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
)

// TransitionError несёт пару статусов, чтобы ответ назвал их явно.
type TransitionError struct {
	From, To Status
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("transition %s -> %s is not allowed", e.From, e.To)
}

func (e *TransitionError) Unwrap() error { return ErrInvalidStatusTransition }

func (e *TransitionError) UserMessage() string {
	return fmt.Sprintf("Переход из %s в %s недопустим", e.From, e.To)
}

type FieldError struct {
	Field   string
	Message string
}

// ValidationError — ошибка входных данных. Если среди проблем есть нарушение
// правил состава, ответ получает код invalid_team_rules.
type ValidationError struct {
	Problems []FieldError
	Rules    bool
}

func (e *ValidationError) Error() string { return "validation failed" }

func (e *ValidationError) Fields() [][2]string {
	out := make([][2]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = [2]string{p.Field, p.Message}
	}
	return out
}

func (e *ValidationError) ErrorCode() (code, message string) {
	if e.Rules {
		return "invalid_team_rules", "Правила состава команды невыполнимы"
	}
	return "", ""
}

// ValidateRules проверяет выполнимость правил состава:
// sum(perTeamMin) <= teamSize <= sum(perTeamMax) и корректность самих ролей.
func ValidateRules(teamSize int, roles []RoleInput) []FieldError {
	var ps []FieldError
	add := func(field, format string, args ...any) {
		ps = append(ps, FieldError{field, fmt.Sprintf(format, args...)})
	}

	if teamSize < 1 {
		add("teamSize", "должен быть не меньше 1")
	}
	if teamSize > maxInt32 {
		add("teamSize", "слишком большое значение")
		teamSize = 0 // дальнейшие проверки сумм бессмысленны
	}
	if len(roles) == 0 {
		add("roles", "нужна хотя бы одна роль")
	}

	roleProblems := false
	seen := map[string]bool{}
	sumMin, sumMax := 0, 0
	for i, r := range roles {
		field := func(name string) string { return fmt.Sprintf("roles[%d].%s", i, name) }
		name := strings.TrimSpace(r.Name)
		switch {
		case name == "":
			add(field("name"), "не может быть пустым")
			roleProblems = true
		case utf8.RuneCountInString(name) > titleMaxLen:
			add(field("name"), "не длиннее 255 символов")
			roleProblems = true
		case seen[strings.ToLower(name)]:
			add(field("name"), "роль «%s» уже есть в списке", name)
			roleProblems = true
		}
		seen[strings.ToLower(name)] = true

		if r.PerTeamMin < 0 {
			add(field("perTeamMin"), "не может быть отрицательным")
			roleProblems = true
		}
		if r.PerTeamMax < 1 {
			add(field("perTeamMax"), "должен быть не меньше 1")
			roleProblems = true
		}
		if r.PerTeamMin > maxInt32 || r.PerTeamMax > maxInt32 {
			add(field("perTeamMax"), "слишком большое значение")
			roleProblems = true
		}
		if r.PerTeamMin >= 0 && r.PerTeamMax >= 1 && r.PerTeamMin > r.PerTeamMax {
			add(field("perTeamMin"), "больше максимума (%d)", r.PerTeamMax)
			roleProblems = true
		}
		sumMin += r.PerTeamMin
		sumMax += r.PerTeamMax
	}

	// суммы осмысленны, только когда сами роли корректны
	if teamSize >= 1 && len(roles) > 0 && !roleProblems {
		if sumMin > teamSize {
			add("teamSize", "сумма минимальных квот (%d) больше размера команды (%d)", sumMin, teamSize)
		}
		if sumMax < teamSize {
			add("teamSize", "сумма максимальных квот (%d) меньше размера команды (%d)", sumMax, teamSize)
		}
	}
	return ps
}

// Normalize обрезает пробелы и подставляет значения по умолчанию.
func (n NewSplit) Normalize() NewSplit {
	n.Title = strings.TrimSpace(n.Title)
	if n.RemainderPolicy == "" {
		n.RemainderPolicy = PolicySpread
	}
	n.Roles = normalizeRoles(n.Roles)
	return n
}

func (n NewSplit) Validate() error {
	var ps []FieldError
	ps = append(ps, validateCommon(&n.Title, n.RemainderPolicy, n.MaxMembers)...)
	rules := ValidateRules(n.TeamSize, n.Roles)
	if len(ps)+len(rules) == 0 {
		return nil
	}
	return &ValidationError{Problems: append(ps, rules...), Rules: len(rules) > 0}
}

func (p Patch) Normalize() Patch {
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		p.Title = &t
	}
	if p.Roles != nil {
		r := normalizeRoles(*p.Roles)
		p.Roles = &r
	}
	return p
}

func (p Patch) ChangesRules(cur Split) bool {
	if p.TeamSize != nil && *p.TeamSize != cur.TeamSize {
		return true
	}
	if p.Roles == nil {
		return false
	}
	next := *p.Roles
	if len(next) != len(cur.Roles) {
		return true
	}
	for i, r := range next {
		c := cur.Roles[i]
		if r.Name != c.Name || r.PerTeamMin != c.PerTeamMin || r.PerTeamMax != c.PerTeamMax {
			return true
		}
	}
	return false
}

// Validate проверяет патч вместе с текущим состоянием: смена одного teamSize
// не должна ломать уже заданные квоты.
func (p Patch) Validate(cur Split, membersCount int) error {
	var ps []FieldError
	policy := PolicySpread
	if p.RemainderPolicy != nil {
		policy = *p.RemainderPolicy
	}
	var maxMembers *int
	if p.MaxMembers.Set {
		maxMembers = p.MaxMembers.Value
	}
	ps = append(ps, validateCommon(p.Title, policy, maxMembers)...)
	if maxMembers != nil && *maxMembers < membersCount {
		ps = append(ps, FieldError{"maxMembers", fmt.Sprintf("меньше текущего числа участников (%d)", membersCount)})
	}

	var rules []FieldError
	if p.TeamSize != nil || p.Roles != nil {
		teamSize := cur.TeamSize
		if p.TeamSize != nil {
			teamSize = *p.TeamSize
		}
		var roles []RoleInput
		if p.Roles != nil {
			roles = *p.Roles
		} else {
			for _, r := range cur.Roles {
				roles = append(roles, RoleInput{Name: r.Name, PerTeamMin: r.PerTeamMin, PerTeamMax: r.PerTeamMax})
			}
		}
		rules = ValidateRules(teamSize, roles)
	}
	if len(ps)+len(rules) == 0 {
		return nil
	}
	return &ValidationError{Problems: append(ps, rules...), Rules: len(rules) > 0}
}

func validateCommon(title *string, policy RemainderPolicy, maxMembers *int) []FieldError {
	var ps []FieldError
	if title != nil {
		switch {
		case *title == "":
			ps = append(ps, FieldError{"title", "не может быть пустым"})
		case utf8.RuneCountInString(*title) > titleMaxLen:
			ps = append(ps, FieldError{"title", "не длиннее 255 символов"})
		}
	}
	if !policy.Valid() {
		ps = append(ps, FieldError{"remainderPolicy", "допустимы smaller_last_team, spread, waitlist"})
	}
	if maxMembers != nil && *maxMembers < 1 {
		ps = append(ps, FieldError{"maxMembers", "должен быть не меньше 1"})
	}
	if maxMembers != nil && *maxMembers > maxInt32 {
		ps = append(ps, FieldError{"maxMembers", "слишком большое значение"})
	}
	return ps
}

func normalizeRoles(in []RoleInput) []RoleInput {
	out := make([]RoleInput, len(in))
	for i, r := range in {
		r.Name = strings.TrimSpace(r.Name)
		if r.SortOrder == nil {
			order := i
			r.SortOrder = &order
		}
		out[i] = r
	}
	return out
}
