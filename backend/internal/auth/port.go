package auth

import "context"

// UserStore — то, что auth требует от модуля пользователей. Типы свои;
// конвертацию делает адаптер в cmd/api.
type UserStore interface {
	Create(ctx context.Context, u NewUser) (User, error)                // ErrLoginTaken
	GetByLogin(ctx context.Context, login string) (UserWithHash, error) // ErrUserNotFound
	GetByID(ctx context.Context, id int64) (User, error)                // ErrUserNotFound
}
