package form

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrNotFound = errors.New("form not found")

const (
	nameMaxLen     = 255
	skillMaxLen    = 100
	skillsMax      = 50
	prefsMax       = 20
	commentMaxLen  = 2000
	experienceMin  = 0
	experienceMax  = 10
	roleNameMaxLen = 255
)

type RolePreference struct {
	RoleName string
	Rank     int
}

type Form struct {
	ID              int64
	UserID          int64
	Name            string
	Experience      int
	Skills          []string
	RolePreferences []RolePreference
	Comment         *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Snapshot — содержимое анкеты, которое замораживается в участнике при
// вступлении. Названия нет: оно видно только владельцу анкеты.
type Snapshot struct {
	Experience      int
	Skills          []string
	RolePreferences []RolePreference
	Comment         *string
}

func (f Form) Snapshot() Snapshot {
	return Snapshot{
		Experience:      f.Experience,
		Skills:          append([]string{}, f.Skills...),
		RolePreferences: append([]RolePreference{}, f.RolePreferences...),
		Comment:         f.Comment,
	}
}

type NewForm struct {
	Name            string
	Experience      int
	Skills          []string
	RolePreferences []RolePreference
	Comment         *string
}

// Optional различает «поле не передано» (Set=false) и «передано» (Set=true);
// Value=nil при Set=true означает явный null, то есть очистку поля.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// Update — частичное обновление. nil у Name/Experience/Skills/RolePreferences
// значит «не менять».
type Update struct {
	Name            *string
	Experience      *int
	Skills          *[]string
	RolePreferences *[]RolePreference
	Comment         Optional[string]
}

func (u Update) IsEmpty() bool {
	return u.Name == nil && u.Experience == nil && u.Skills == nil &&
		u.RolePreferences == nil && !u.Comment.Set
}

// Normalize обрезает пробелы, убирает пустые и повторяющиеся навыки
// (без учёта регистра, первое написание сохраняется) и сортирует предпочтения
// по рангу. Пустой комментарий означает очистку.
func (n NewForm) Normalize() NewForm {
	n.Name = strings.TrimSpace(n.Name)
	n.Skills = normalizeSkills(n.Skills)
	n.RolePreferences = normalizePrefs(n.RolePreferences)
	n.Comment = normalizeComment(n.Comment)
	return n
}

func (n NewForm) Validate() error {
	var v validator
	v.name(&n.Name, true)
	v.experience(&n.Experience)
	v.skills(n.Skills)
	v.prefs(n.RolePreferences)
	v.comment(n.Comment)
	return v.err()
}

func (u Update) Normalize() Update {
	if u.Name != nil {
		t := strings.TrimSpace(*u.Name)
		u.Name = &t
	}
	if u.Skills != nil {
		s := normalizeSkills(*u.Skills)
		u.Skills = &s
	}
	if u.RolePreferences != nil {
		p := normalizePrefs(*u.RolePreferences)
		u.RolePreferences = &p
	}
	u.Comment.Value = normalizeComment(u.Comment.Value)
	return u
}

func (u Update) Validate() error {
	var v validator
	v.name(u.Name, true)
	v.experience(u.Experience)
	if u.Skills != nil {
		v.skills(*u.Skills)
	}
	if u.RolePreferences != nil {
		v.prefs(*u.RolePreferences)
	}
	v.comment(u.Comment.Value)
	return v.err()
}

type validator struct{ ps []FieldError }

func (v *validator) add(field, msg string) { v.ps = append(v.ps, FieldError{field, msg}) }

func (v *validator) err() error {
	if len(v.ps) == 0 {
		return nil
	}
	return &ValidationError{Problems: v.ps}
}

func (v *validator) name(s *string, required bool) {
	if s == nil {
		return
	}
	switch {
	case required && *s == "":
		v.add("name", "не может быть пустым")
	case utf8.RuneCountInString(*s) > nameMaxLen:
		v.add("name", "не длиннее 255 символов")
	}
}

func (v *validator) experience(e *int) {
	if e != nil && (*e < experienceMin || *e > experienceMax) {
		v.add("experience", "допустимы значения от 0 до 10")
	}
}

func (v *validator) skills(skills []string) {
	if len(skills) > skillsMax {
		v.add("skills", "не больше 50 навыков")
	}
	for i, s := range skills {
		if utf8.RuneCountInString(s) > skillMaxLen {
			v.add(fmt.Sprintf("skills[%d]", i), "не длиннее 100 символов")
		}
	}
}

func (v *validator) prefs(prefs []RolePreference) {
	if len(prefs) > prefsMax {
		v.add("rolePreferences", "не больше 20 ролей")
	}
	names := map[string]bool{}
	ranks := map[int]bool{}
	for i, p := range prefs {
		field := fmt.Sprintf("rolePreferences[%d]", i)
		switch {
		case p.RoleName == "":
			v.add(field+".roleName", "не может быть пустым")
		case utf8.RuneCountInString(p.RoleName) > roleNameMaxLen:
			v.add(field+".roleName", "не длиннее 255 символов")
		case names[strings.ToLower(p.RoleName)]:
			v.add(field+".roleName", "роль указана повторно")
		}
		names[strings.ToLower(p.RoleName)] = true
		switch {
		case p.Rank < 1:
			v.add(field+".rank", "должен быть не меньше 1")
		case ranks[p.Rank]:
			v.add(field+".rank", "ранг указан повторно")
		}
		ranks[p.Rank] = true
	}
}

func (v *validator) comment(c *string) {
	if c != nil && utf8.RuneCountInString(*c) > commentMaxLen {
		v.add("comment", "не длиннее 2000 символов")
	}
}

func normalizeSkills(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func normalizePrefs(in []RolePreference) []RolePreference {
	out := make([]RolePreference, len(in))
	for i, p := range in {
		out[i] = RolePreference{RoleName: strings.TrimSpace(p.RoleName), Rank: p.Rank}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

func normalizeComment(c *string) *string {
	if c == nil {
		return nil
	}
	t := strings.TrimSpace(*c)
	if t == "" {
		return nil
	}
	return &t
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
