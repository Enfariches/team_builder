package tests

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"builder/backend/internal/form"
)

func ptr[T any](v T) *T { return &v }

func fieldsOf(t *testing.T, err error) []string {
	t.Helper()
	var ve *form.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("ошибка %v не ValidationError", err)
	}
	var out []string
	for _, p := range ve.Problems {
		out = append(out, p.Field)
	}
	return out
}

func TestNewFormValidate(t *testing.T) {
	prefs := func(n int) []form.RolePreference {
		out := make([]form.RolePreference, n)
		for i := range out {
			out[i] = form.RolePreference{RoleName: strings.Repeat("r", i+1), Rank: i + 1}
		}
		return out
	}
	tests := []struct {
		name string
		in   form.NewForm
		want []string
	}{
		{"минимальная", form.NewForm{Name: "a"}, nil},
		{"пустое имя", form.NewForm{Name: "  "}, []string{"name"}},
		{"длинное имя", form.NewForm{Name: strings.Repeat("я", 256)}, []string{"name"}},
		{"имя 255", form.NewForm{Name: strings.Repeat("я", 255)}, nil},
		{"опыт -1", form.NewForm{Name: "a", Experience: -1}, []string{"experience"}},
		{"опыт 11", form.NewForm{Name: "a", Experience: 11}, []string{"experience"}},
		{"опыт 10", form.NewForm{Name: "a", Experience: 10}, nil},
		{"длинный навык", form.NewForm{Name: "a", Skills: []string{"Go", strings.Repeat("x", 101)}}, []string{"skills[1]"}},
		{"много навыков", form.NewForm{Name: "a", Skills: func() []string {
			s := make([]string, 51)
			for i := range s {
				s[i] = strings.Repeat("x", i+1)
			}
			return s
		}()}, []string{"skills"}},
		{"ранг 0", form.NewForm{Name: "a", RolePreferences: []form.RolePreference{{RoleName: "Go", Rank: 0}}}, []string{"rolePreferences[0].rank"}},
		{"дубль ранга", form.NewForm{Name: "a", RolePreferences: []form.RolePreference{{RoleName: "A", Rank: 1}, {RoleName: "B", Rank: 1}}}, []string{"rolePreferences[1].rank"}},
		{"дубль роли без регистра", form.NewForm{Name: "a", RolePreferences: []form.RolePreference{{RoleName: "Backend", Rank: 1}, {RoleName: "backend", Rank: 2}}}, []string{"rolePreferences[1].roleName"}},
		{"пустая роль", form.NewForm{Name: "a", RolePreferences: []form.RolePreference{{RoleName: " ", Rank: 1}}}, []string{"rolePreferences[0].roleName"}},
		{"21 предпочтение", form.NewForm{Name: "a", RolePreferences: prefs(21)}, []string{"rolePreferences"}},
		{"длинный комментарий", form.NewForm{Name: "a", Comment: ptr(strings.Repeat("я", 2001))}, []string{"comment"}},
		{"несколько проблем", form.NewForm{Name: "", Experience: 99}, []string{"name", "experience"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Normalize().Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				return
			}
			if got := fieldsOf(t, err); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("поля = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	got := form.NewForm{
		Name:            "  Моя  ",
		Skills:          []string{" Go ", "go", "", "  ", "SQL", "Go"},
		RolePreferences: []form.RolePreference{{RoleName: " B ", Rank: 2}, {RoleName: "A", Rank: 1}},
		Comment:         ptr("   "),
	}.Normalize()

	if got.Name != "Моя" {
		t.Errorf("name = %q", got.Name)
	}
	if want := []string{"Go", "SQL"}; !reflect.DeepEqual(got.Skills, want) {
		t.Errorf("skills = %v, want %v", got.Skills, want)
	}
	if want := []form.RolePreference{{RoleName: "A", Rank: 1}, {RoleName: "B", Rank: 2}}; !reflect.DeepEqual(got.RolePreferences, want) {
		t.Errorf("prefs = %v, want %v", got.RolePreferences, want)
	}
	if got.Comment != nil {
		t.Errorf("пустой comment должен стать nil, got %q", *got.Comment)
	}
}

func TestUpdateValidate(t *testing.T) {
	tests := []struct {
		name string
		in   form.Update
		want []string
	}{
		{"пустой патч", form.Update{}, nil},
		{"пустое имя", form.Update{Name: ptr("")}, []string{"name"}},
		{"опыт вне диапазона", form.Update{Experience: ptr(11)}, []string{"experience"}},
		{"дубль ранга", form.Update{RolePreferences: &[]form.RolePreference{{RoleName: "A", Rank: 1}, {RoleName: "B", Rank: 1}}}, []string{"rolePreferences[1].rank"}},
		{"очистка навыков допустима", form.Update{Skills: &[]string{}}, nil},
		{"null comment допустим", form.Update{Comment: form.Optional[string]{Set: true}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Normalize().Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				return
			}
			if got := fieldsOf(t, err); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("поля = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateIsEmpty(t *testing.T) {
	if !(form.Update{}).IsEmpty() {
		t.Error("пустой Update должен быть IsEmpty")
	}
	if (form.Update{Comment: form.Optional[string]{Set: true}}).IsEmpty() {
		t.Error("comment: null — это изменение")
	}
}

func TestSnapshotOmitsNameAndCopies(t *testing.T) {
	f := form.Form{Name: "секрет", Experience: 3, Skills: []string{"Go"}, RolePreferences: []form.RolePreference{{RoleName: "A", Rank: 1}}}
	s := f.Snapshot()
	s.Skills[0] = "изменено"
	if f.Skills[0] != "Go" {
		t.Fatal("снимок делит память с анкетой")
	}
}
