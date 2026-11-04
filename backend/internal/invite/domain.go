package invite

import (
	"errors"
	"time"
)

var (
	// ErrNotFound — инвайта с таким id нет в этом пространстве.
	ErrNotFound = errors.New("invite not found")
	// ErrInviteInvalid — токен неизвестен, отозван, истёк или исчерпан;
	// причину наружу намеренно не различаем.
	ErrInviteInvalid = errors.New("invite is not valid")
	// ErrSplitNotFound — пространства нет или у пользователя нет к нему доступа.
	ErrSplitNotFound = errors.New("split not found")
	ErrForbidden     = errors.New("owner only")
)

// Access — роль пользователя в пространстве, как её отдаёт порт SplitAccess.
type Access string

const (
	AccessOwner  Access = "Owner"
	AccessMember Access = "Member"
)

type Invite struct {
	ID        int64
	SplitID   int64
	Token     string
	ExpiresAt *time.Time
	MaxUses   *int
	UsesCount int
	RevokedAt *time.Time
	CreatedAt time.Time
}

// ValidAt: инвайт можно использовать, если он не отозван, не истёк и не исчерпан.
// Истечение строгое: в момент expiresAt инвайт уже недействителен.
func (i Invite) ValidAt(now time.Time) bool {
	switch {
	case i.RevokedAt != nil:
		return false
	case i.ExpiresAt != nil && !i.ExpiresAt.After(now):
		return false
	case i.MaxUses != nil && i.UsesCount >= *i.MaxUses:
		return false
	}
	return true
}

type NewInvite struct {
	ExpiresAt *time.Time
	MaxUses   *int
}

func (n NewInvite) Validate(now time.Time) error {
	var ps []FieldError
	if n.ExpiresAt != nil && !n.ExpiresAt.After(now) {
		ps = append(ps, FieldError{"expiresAt", "должно быть в будущем"})
	}
	if n.MaxUses != nil && *n.MaxUses < 1 {
		ps = append(ps, FieldError{"maxUses", "должно быть не меньше 1"})
	}
	if len(ps) == 0 {
		return nil
	}
	return &ValidationError{Problems: ps}
}

// SplitInfo — публичные сведения о пространстве для предпросмотра.
type SplitInfo struct {
	Title        string
	Description  *string
	TeamSize     int
	RoleNames    []string
	MembersCount int
	Status       string
}

// Redemption — результат успешного использования инвайта.
type Redemption struct {
	InviteID int64
	SplitID  int64
}

type FieldError struct {
	Field   string
	Message string
}

type ValidationError struct {
	Problems []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

// Fields нужен platform.NewErrorHandler для ответа 422 с details.
func (e *ValidationError) Fields() [][2]string {
	out := make([][2]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = [2]string{p.Field, p.Message}
	}
	return out
}
