package tests

import (
	"testing"

	"builder/backend/internal/auth"
)

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{"верный пароль", "correct horse", hash, true},
		{"неверный пароль", "wrong", hash, false},
		{"пустой хеш", "correct horse", "", false},
		{"мусор вместо хеша", "correct horse", "$argon2id$garbage", false},
		{"другой алгоритм", "correct horse", "$bcrypt$v=19$m=1,t=1,p=1$YQ$YQ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auth.VerifyPassword(tt.password, tt.hash); got != tt.want {
				t.Fatalf("auth.VerifyPassword = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	a, _ := auth.HashPassword("same")
	b, _ := auth.HashPassword("same")
	if a == b {
		t.Fatal("одинаковые хеши: соль не случайная")
	}
}
