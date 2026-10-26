package user

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrLoginTaken = errors.New("login taken")
	ErrNotFound   = errors.New("user not found")
)

type User struct {
	ID         int64
	Login      string
	FirstName  string
	MiddleName *string
	LastName   string
	Link       *string
	CreatedAt  time.Time
}

// Account — пользователь вместе с хешем пароля; наружу из модуля отдаётся
// только тем, кто проверяет учётные данные.
type Account struct {
	User
	PasswordHash string
}

type NewAccount struct {
	Login        string
	PasswordHash string
	FirstName    string
	MiddleName   *string
	LastName     string
	Link         *string
}

const nameMaxLen = 255

// Optional различает «поле не передано» (Set=false) и «передано» (Set=true);
// Value=nil при Set=true означает явный null, то есть очистку поля.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// ProfileUpdate — частичное обновление профиля. nil у FirstName/LastName
// значит «не менять»: для обязательных полей null и отсутствие равнозначны.
type ProfileUpdate struct {
	FirstName  *string
	LastName   *string
	MiddleName Optional[string]
	Link       Optional[string]
}

func (u ProfileUpdate) IsEmpty() bool {
	return u.FirstName == nil && u.LastName == nil && !u.MiddleName.Set && !u.Link.Set
}

// Normalize обрезает пробелы; пустая строка в необязательных полях означает
// очистку, а в обязательных остаётся пустой и ловится Validate.
func (u ProfileUpdate) Normalize() ProfileUpdate {
	u.FirstName = trimPtr(u.FirstName)
	u.LastName = trimPtr(u.LastName)
	u.MiddleName = normalizeOptional(u.MiddleName)
	u.Link = normalizeOptional(u.Link)
	return u
}

func (u ProfileUpdate) Validate() error {
	var ps []FieldError
	add := func(field, msg string) { ps = append(ps, FieldError{field, msg}) }

	for _, f := range []struct {
		name     string
		v        *string
		required bool
	}{
		{"firstName", u.FirstName, true},
		{"lastName", u.LastName, true},
		{"middleName", u.MiddleName.Value, false},
		{"link", u.Link.Value, false},
	} {
		if f.v == nil {
			continue
		}
		switch {
		case f.required && *f.v == "":
			add(f.name, "не может быть пустым")
		case utf8.RuneCountInString(*f.v) > nameMaxLen:
			add(f.name, "не длиннее 255 символов")
		}
	}

	if len(ps) == 0 {
		return nil
	}
	return &ValidationError{Problems: ps}
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

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func normalizeOptional(o Optional[string]) Optional[string] {
	o.Value = trimPtr(o.Value)
	if o.Value != nil && *o.Value == "" {
		o.Value = nil
	}
	return o
}
