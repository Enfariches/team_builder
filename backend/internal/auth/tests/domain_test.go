package tests

import (
	"builder/backend/internal/auth"
	"errors"
	"strings"
	"testing"
)

func TestRegisterInputValidate(t *testing.T) {
	ok := auth.RegisterInput{Login: "ivan", Password: "12345678", FirstName: "Иван", LastName: "Иванов"}
	long := strings.Repeat("я", 256)

	tests := []struct {
		name       string
		mutate     func(*auth.RegisterInput)
		wantFields []string
	}{
		{"валидный", func(*auth.RegisterInput) {}, nil},
		{"короткий логин", func(in *auth.RegisterInput) { in.Login = "ab" }, []string{"login"}},
		{"короткий пароль", func(in *auth.RegisterInput) { in.Password = "1234567" }, []string{"password"}},
		{"длинный пароль", func(in *auth.RegisterInput) { in.Password = strings.Repeat("a", 129) }, []string{"password"}},
		{"пустое имя", func(in *auth.RegisterInput) { in.FirstName = "  " }, []string{"firstName"}},
		{"длинная фамилия", func(in *auth.RegisterInput) { in.LastName = long }, []string{"lastName"}},
		{"длинная ссылка", func(in *auth.RegisterInput) { in.Link = &long }, []string{"link"}},
		{"кириллический логин считается рунами", func(in *auth.RegisterInput) { in.Login = "аб" }, []string{"login"}},
		{"несколько ошибок", func(in *auth.RegisterInput) { in.Login = "a"; in.Password = "b" }, []string{"login", "password"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := ok
			tt.mutate(&in)
			err := in.Validate()
			if tt.wantFields == nil {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				return
			}
			var ve *auth.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("ожидалась auth.ValidationError, получено %v", err)
			}
			got := map[string]bool{}
			for _, p := range ve.Problems {
				got[p.Field] = true
			}
			if len(got) != len(tt.wantFields) {
				t.Fatalf("поля = %v, want %v", got, tt.wantFields)
			}
			for _, f := range tt.wantFields {
				if !got[f] {
					t.Fatalf("нет ошибки по полю %s: %v", f, got)
				}
			}
		})
	}
}
