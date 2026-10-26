package tests

import (
	"errors"
	"strings"
	"testing"

	"builder/backend/internal/user"
)

func ptr[T any](v T) *T { return &v }

func opt(v *string) user.Optional[string] { return user.Optional[string]{Set: true, Value: v} }

func TestProfileUpdateIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		u    user.ProfileUpdate
		want bool
	}{
		{"ничего не передано", user.ProfileUpdate{}, true},
		{"имя", user.ProfileUpdate{FirstName: ptr("Пётр")}, false},
		{"null в link", user.ProfileUpdate{Link: opt(nil)}, false},
		{"значение в middleName", user.ProfileUpdate{MiddleName: opt(ptr("И"))}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.u.IsEmpty(); got != tt.want {
				t.Fatalf("IsEmpty = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProfileUpdateNormalize(t *testing.T) {
	u := user.ProfileUpdate{
		FirstName:  ptr("  Пётр "),
		LastName:   ptr("   "),
		MiddleName: opt(ptr("  ")),
		Link:       opt(ptr(" @tg ")),
	}.Normalize()

	if *u.FirstName != "Пётр" {
		t.Errorf("FirstName = %q", *u.FirstName)
	}
	if *u.LastName != "" {
		t.Errorf("LastName = %q, обязательное поле остаётся пустым для Validate", *u.LastName)
	}
	if !u.MiddleName.Set || u.MiddleName.Value != nil {
		t.Errorf("пустая строка в middleName должна стать очисткой: %+v", u.MiddleName)
	}
	if *u.Link.Value != "@tg" {
		t.Errorf("Link = %q", *u.Link.Value)
	}
}

func TestProfileUpdateValidate(t *testing.T) {
	long := strings.Repeat("я", 256)
	tests := []struct {
		name       string
		u          user.ProfileUpdate
		wantFields []string
	}{
		{"пустой патч", user.ProfileUpdate{}, nil},
		{"валидный", user.ProfileUpdate{FirstName: ptr("Пётр"), Link: opt(ptr("@tg"))}, nil},
		{"очистка необязательных", user.ProfileUpdate{MiddleName: opt(nil), Link: opt(nil)}, nil},
		{"пустое имя", user.ProfileUpdate{FirstName: ptr("")}, []string{"firstName"}},
		{"пустая фамилия", user.ProfileUpdate{LastName: ptr("")}, []string{"lastName"}},
		{"длинное имя", user.ProfileUpdate{FirstName: ptr(long)}, []string{"firstName"}},
		{"длинный link", user.ProfileUpdate{Link: opt(ptr(long))}, []string{"link"}},
		{"255 рун допустимо", user.ProfileUpdate{FirstName: ptr(strings.Repeat("я", 255))}, nil},
		{"несколько ошибок", user.ProfileUpdate{FirstName: ptr(""), MiddleName: opt(ptr(long))}, []string{"firstName", "middleName"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.u.Validate()
			if tt.wantFields == nil {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				return
			}
			var ve *user.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
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
