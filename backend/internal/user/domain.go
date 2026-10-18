package user

import (
	"errors"
	"time"
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
