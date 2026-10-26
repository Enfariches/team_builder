package tests

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"builder/backend/internal/split"
)

func ptr[T any](v T) *T { return &v }

func roles(spec ...[2]int) []split.RoleInput {
	out := make([]split.RoleInput, len(spec))
	for i, s := range spec {
		out[i] = split.RoleInput{Name: fmt.Sprintf("Role%d", i), PerTeamMin: s[0], PerTeamMax: s[1]}
	}
	return out
}

func fieldsOf(ps []split.FieldError) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.Field] = true
	}
	return m
}

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name       string
		teamSize   int
		roles      []split.RoleInput
		wantFields []string
		wantMsg    string
	}{
		{"пример из спеки", 5, roles([2]int{1, 2}, [2]int{1, 2}, [2]int{1, 1}, [2]int{0, 1}), nil, ""},
		{"границы: min равен размеру", 3, roles([2]int{3, 3}), nil, ""},
		{"сумма минимумов больше размера", 5, roles([2]int{3, 3}, [2]int{3, 3}), []string{"teamSize"},
			"сумма минимальных квот (6) больше размера команды (5)"},
		{"сумма максимумов меньше размера", 5, roles([2]int{0, 1}, [2]int{0, 1}), []string{"teamSize"},
			"сумма максимальных квот (2) меньше размера команды (5)"},
		{"нулевой размер", 0, roles([2]int{0, 1}), []string{"teamSize"}, ""},
		{"нет ролей", 3, nil, []string{"roles"}, ""},
		{"min больше max", 5, roles([2]int{3, 2}), []string{"roles[0].perTeamMin"}, ""},
		{"max меньше 1", 5, roles([2]int{0, 0}), []string{"roles[0].perTeamMax"}, ""},
		{"отрицательный min", 5, roles([2]int{-1, 2}), []string{"roles[0].perTeamMin"}, ""},
		{"пустое имя", 5, []split.RoleInput{{Name: "  ", PerTeamMin: 0, PerTeamMax: 5}}, []string{"roles[0].name"}, ""},
		{"дубль имени без учёта регистра", 5, []split.RoleInput{
			{Name: "Backend", PerTeamMax: 3}, {Name: " backend ", PerTeamMax: 3}}, []string{"roles[1].name"}, ""},
		{"огромные значения", 1 << 40, roles([2]int{0, 1 << 40}), []string{"teamSize", "roles[0].perTeamMax"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := split.ValidateRules(tt.teamSize, tt.roles)
			got := fieldsOf(ps)
			if len(tt.wantFields) == 0 && len(ps) != 0 {
				t.Fatalf("неожиданные проблемы: %+v", ps)
			}
			for _, f := range tt.wantFields {
				if !got[f] {
					t.Errorf("нет проблемы по полю %s: %+v", f, ps)
				}
			}
			if tt.wantMsg != "" && (len(ps) == 0 || ps[0].Message != tt.wantMsg) {
				t.Errorf("сообщение = %+v, want %q", ps, tt.wantMsg)
			}
		})
	}
}

// Формула sum(min) <= teamSize <= sum(max) должна совпадать с прямым
// определением: существует набор количеств по ролям внутри квот с суммой teamSize.
func TestValidateRulesMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	feasible := func(teamSize int, rs []split.RoleInput) bool {
		reach := map[int]bool{0: true}
		for _, r := range rs {
			next := map[int]bool{}
			for sum := range reach {
				for c := r.PerTeamMin; c <= r.PerTeamMax; c++ {
					next[sum+c] = true
				}
			}
			reach = next
		}
		return reach[teamSize]
	}
	for i := 0; i < 2000; i++ {
		n := 1 + rng.Intn(4)
		rs := make([]split.RoleInput, n)
		for j := range rs {
			lo := rng.Intn(4)
			rs[j] = split.RoleInput{Name: fmt.Sprintf("r%d", j), PerTeamMin: lo, PerTeamMax: max(lo, 1) + rng.Intn(3)}
		}
		teamSize := 1 + rng.Intn(12)
		want := feasible(teamSize, rs)
		got := len(split.ValidateRules(teamSize, rs)) == 0
		if got != want {
			t.Fatalf("teamSize=%d roles=%+v: валидатор=%v, перебор=%v", teamSize, rs, got, want)
		}
	}
}

func TestStatusTransitions(t *testing.T) {
	all := []split.Status{split.StatusDraft, split.StatusOpen, split.StatusClosed, split.StatusFinished}
	allowed := map[[2]split.Status]bool{
		{split.StatusDraft, split.StatusOpen}:  true,
		{split.StatusOpen, split.StatusClosed}: true,
		{split.StatusClosed, split.StatusOpen}: true,
	}
	for _, from := range all {
		for _, to := range all {
			if got := from.CanTransition(to); got != allowed[[2]split.Status{from, to}] {
				t.Errorf("%s -> %s: %v, want %v", from, to, got, !got)
			}
		}
	}
}

func TestNewSplitNormalizeAndValidate(t *testing.T) {
	n := split.NewSplit{Title: "  Практикум ", TeamSize: 3, Roles: roles([2]int{1, 3})}.Normalize()
	if n.Title != "Практикум" || n.RemainderPolicy != split.PolicySpread {
		t.Fatalf("normalize: %+v", n)
	}
	if n.Roles[0].SortOrder == nil || *n.Roles[0].SortOrder != 0 {
		t.Fatalf("sortOrder по умолчанию: %+v", n.Roles[0])
	}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}

	bad := split.NewSplit{Title: " ", TeamSize: 3, MaxMembers: ptr(0), RemainderPolicy: "x", Roles: roles([2]int{5, 5})}.Normalize()
	var ve *split.ValidationError
	if err := bad.Validate(); !errors.As(err, &ve) || !ve.Rules {
		t.Fatalf("err = %v, want ValidationError с Rules", err)
	}
	if code, _ := ve.ErrorCode(); code != "invalid_team_rules" {
		t.Fatalf("code = %q", code)
	}
	got := fieldsOf(ve.Problems)
	for _, f := range []string{"title", "maxMembers", "remainderPolicy", "teamSize"} {
		if !got[f] {
			t.Errorf("нет проблемы по полю %s: %+v", f, ve.Problems)
		}
	}

	onlyField := split.NewSplit{Title: "", TeamSize: 3, Roles: roles([2]int{1, 3})}.Normalize()
	if err := onlyField.Validate(); !errors.As(err, &ve) || ve.Rules {
		t.Fatalf("пустой title не должен давать invalid_team_rules: %v", err)
	}
}

func TestPatchChangesRulesAndValidate(t *testing.T) {
	cur := split.Split{TeamSize: 5, Roles: []split.Role{
		{Name: "Backend", PerTeamMin: 1, PerTeamMax: 3},
		{Name: "Frontend", PerTeamMin: 2, PerTeamMax: 3},
	}}

	t.Run("ChangesRules", func(t *testing.T) {
		same := split.Patch{TeamSize: ptr(5), Roles: &[]split.RoleInput{
			{Name: "Backend", PerTeamMin: 1, PerTeamMax: 3}, {Name: "Frontend", PerTeamMin: 2, PerTeamMax: 3}}}
		tests := []struct {
			name string
			p    split.Patch
			want bool
		}{
			{"пусто", split.Patch{}, false},
			{"те же значения", same, false},
			{"другой размер", split.Patch{TeamSize: ptr(6)}, true},
			{"другие квоты", split.Patch{Roles: &[]split.RoleInput{{Name: "Backend", PerTeamMin: 1, PerTeamMax: 4}, {Name: "Frontend", PerTeamMin: 2, PerTeamMax: 3}}}, true},
			{"другое число ролей", split.Patch{Roles: &[]split.RoleInput{{Name: "Backend", PerTeamMin: 1, PerTeamMax: 3}}}, true},
			{"только title", split.Patch{Title: ptr("X")}, false},
		}
		for _, tt := range tests {
			if got := tt.p.ChangesRules(cur); got != tt.want {
				t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
			}
		}
	})

	t.Run("Validate сливает с текущим состоянием", func(t *testing.T) {
		// минимумы 1+2=3, поэтому размер 2 невыполним, хотя сам по себе корректен
		var ve *split.ValidationError
		if err := (split.Patch{TeamSize: ptr(2)}).Validate(cur, 0); !errors.As(err, &ve) || !ve.Rules {
			t.Fatalf("err = %v", err)
		}
		if err := (split.Patch{TeamSize: ptr(4)}).Validate(cur, 0); err != nil {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("maxMembers не ниже числа участников", func(t *testing.T) {
		p := split.Patch{MaxMembers: split.Optional[int]{Set: true, Value: ptr(3)}}
		var ve *split.ValidationError
		if err := p.Validate(cur, 5); !errors.As(err, &ve) || !fieldsOf(ve.Problems)["maxMembers"] {
			t.Fatalf("err = %v", err)
		}
		if err := p.Validate(cur, 3); err != nil {
			t.Fatalf("err = %v", err)
		}
		null := split.Patch{MaxMembers: split.Optional[int]{Set: true}}
		if err := null.Validate(cur, 99); err != nil {
			t.Fatalf("снятие ограничения: %v", err)
		}
	})
}
