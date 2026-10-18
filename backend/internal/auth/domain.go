package auth

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLoginTaken         = errors.New("login taken")
	ErrUserNotFound       = errors.New("user not found")
	// ErrUnauthorized — нет, просрочен или не прошёл проверку access/refresh-токен.
	ErrUnauthorized  = errors.New("unauthorized")
	ErrTokenNotFound = errors.New("refresh token not found")
)

const (
	loginMinLen    = 3
	loginMaxLen    = 64
	passwordMinLen = 8
	passwordMaxLen = 128
	nameMaxLen     = 255
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

type UserWithHash struct {
	User
	PasswordHash string
}

type NewUser struct {
	Login        string
	PasswordHash string
	FirstName    string
	MiddleName   *string
	LastName     string
	Link         *string
}

type RegisterInput struct {
	Login      string
	Password   string
	FirstName  string
	MiddleName *string
	LastName   string
	Link       *string
}

// Session — результат входа: access отдаётся в теле, refresh — в cookie.
type Session struct {
	AccessToken      string
	ExpiresIn        time.Duration
	RefreshToken     string
	RefreshExpiresAt time.Time
	User             User
}

// RefreshToken — запись о выданном refresh-токене. Сам токен не хранится,
// только его хеш.
type RefreshToken struct {
	ID        int64
	UserID    int64
	Hash      []byte
	FamilyID  string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type FieldError struct {
	Field   string
	Message string
}

type ValidationError struct {
	Problems []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

func (e *ValidationError) Fields() [][2]string {
	out := make([][2]string, len(e.Problems))
	for i, p := range e.Problems {
		out[i] = [2]string{p.Field, p.Message}
	}
	return out
}

func (in RegisterInput) Validate() error {
	var ps []FieldError
	add := func(field, msg string) { ps = append(ps, FieldError{field, msg}) }

	if n := utf8.RuneCountInString(in.Login); n < loginMinLen || n > loginMaxLen {
		add("login", "должен содержать от 3 до 64 символов")
	}
	if n := utf8.RuneCountInString(in.Password); n < passwordMinLen || n > passwordMaxLen {
		add("password", "должен содержать от 8 до 128 символов")
	}
	if strings.TrimSpace(in.FirstName) == "" {
		add("firstName", "обязательное поле")
	}
	if strings.TrimSpace(in.LastName) == "" {
		add("lastName", "обязательное поле")
	}
	for field, v := range map[string]*string{
		"firstName": &in.FirstName, "lastName": &in.LastName,
		"middleName": in.MiddleName, "link": in.Link,
	} {
		if v != nil && utf8.RuneCountInString(*v) > nameMaxLen {
			add(field, "не длиннее 255 символов")
		}
	}

	if len(ps) == 0 {
		return nil
	}
	return &ValidationError{Problems: ps}
}
