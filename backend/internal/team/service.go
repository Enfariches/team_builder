package team

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"strings"
)

// Store — то, что сервису нужно от репозитория. Запись работает в транзакции
// из ctx, если она там есть.
type Store interface {
	Create(ctx context.Context, splitID int64, names []string) ([]int64, error)
	List(ctx context.Context, splitID int64) ([]Team, error) // без участников
	Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error)
}

type Service struct {
	store   Store
	splits  SplitReader
	members MemberDirectory
	users   UserDirectory
}

func NewService(store Store, splits SplitReader, members MemberDirectory, users UserDirectory) *Service {
	return &Service{store: store, splits: splits, members: members, users: users}
}

// CreateTeams создаёт команды публикуемого распределения; ids идут в порядке
// names. Вызывается distribution в общей транзакции.
func (s *Service) CreateTeams(ctx context.Context, splitID int64, names []string) ([]int64, error) {
	ids, err := s.store.Create(ctx, splitID, names)
	if err != nil {
		return nil, fmt.Errorf("team.Service.CreateTeams: %w", err)
	}
	return ids, nil
}

// Counts возвращает число команд по пространствам; отсутствие ключа — ноль.
func (s *Service) Counts(ctx context.Context, splitIDs []int64) (map[int64]int, error) {
	m, err := s.store.Counts(ctx, splitIDs)
	if err != nil {
		return nil, fmt.Errorf("team.Service.Counts: %w", err)
	}
	return m, nil
}

// List — все команды с составами, только владельцу и только после публикации.
func (s *Service) List(ctx context.Context, userID, splitID int64) (Roster, error) {
	r, err := s.loadForOwner(ctx, userID, splitID)
	if err != nil {
		return Roster{}, fmt.Errorf("team.Service.List: %w", err)
	}
	return r, nil
}

// My возвращает команду пользователя с контактами сокомандников. Анкет здесь
// нет в принципе: модуль их не получает.
func (s *Service) My(ctx context.Context, userID, splitID int64) (Team, error) {
	st, err := s.splits.State(ctx, splitID)
	if err != nil {
		return Team{}, fmt.Errorf("team.Service.My: %w", err)
	}
	if st.OwnerID != userID {
		ok, err := s.members.IsMember(ctx, splitID, userID)
		if err != nil {
			return Team{}, fmt.Errorf("team.Service.My: check membership: %w", err)
		}
		if !ok {
			return Team{}, fmt.Errorf("team.Service.My: %w", ErrSplitNotFound)
		}
	}
	if st.Status != StatusFinished {
		return Team{}, fmt.Errorf("team.Service.My: %w", ErrNotFinished)
	}
	r, err := s.load(ctx, splitID)
	if err != nil {
		return Team{}, fmt.Errorf("team.Service.My: %w", err)
	}
	for _, t := range r.Teams {
		for _, m := range t.Members {
			if m.User.ID == userID {
				return t, nil
			}
		}
	}
	return Team{}, fmt.Errorf("team.Service.My: %w", ErrNotAssigned)
}

// Export — CSV с составами команд, только владельцу и после публикации.
func (s *Service) Export(ctx context.Context, userID, splitID int64) ([]byte, error) {
	r, err := s.loadForOwner(ctx, userID, splitID)
	if err != nil {
		return nil, fmt.Errorf("team.Service.Export: %w", err)
	}
	return buildCSV(r), nil
}

func (s *Service) loadForOwner(ctx context.Context, userID, splitID int64) (Roster, error) {
	st, err := s.splits.State(ctx, splitID)
	if err != nil {
		return Roster{}, err
	}
	if st.OwnerID != userID {
		ok, err := s.members.IsMember(ctx, splitID, userID)
		if err != nil {
			return Roster{}, fmt.Errorf("check membership: %w", err)
		}
		if ok {
			return Roster{}, ErrForbidden
		}
		return Roster{}, ErrSplitNotFound
	}
	if st.Status != StatusFinished {
		return Roster{}, ErrNotFinished
	}
	return s.load(ctx, splitID)
}

func (s *Service) load(ctx context.Context, splitID int64) (Roster, error) {
	teams, err := s.store.List(ctx, splitID)
	if err != nil {
		return Roster{}, err
	}
	assigns, err := s.members.ListAssignments(ctx, splitID)
	if err != nil {
		return Roster{}, fmt.Errorf("list assignments: %w", err)
	}
	ids := make([]int64, len(assigns))
	for i, a := range assigns {
		ids[i] = a.UserID
	}
	briefs, err := s.users.ListBriefs(ctx, ids)
	if err != nil {
		return Roster{}, fmt.Errorf("list user briefs: %w", err)
	}
	byUser := make(map[int64]UserBrief, len(briefs))
	for _, b := range briefs {
		byUser[b.ID] = b
	}

	idx := make(map[int64]int, len(teams))
	for i, t := range teams {
		idx[t.ID] = i
		teams[i].Members = []Member{}
	}
	r := Roster{Teams: teams, Waitlist: []Waitlisted{}}
	for _, a := range assigns {
		u := byUser[a.UserID]
		if a.TeamID == nil {
			r.Waitlist = append(r.Waitlist, Waitlisted{MemberID: a.MemberID, User: u})
			continue
		}
		if i, ok := idx[*a.TeamID]; ok {
			r.Teams[i].Members = append(r.Teams[i].Members, Member{MemberID: a.MemberID, User: u, Role: a.Role})
		}
	}
	return r, nil
}

// utf8BOM нужен Excel, иначе кириллица в CSV открывается кракозябрами.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func buildCSV(r Roster) []byte {
	var buf bytes.Buffer
	buf.Write(utf8BOM)
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Команда", "Фамилия", "Имя", "Контакт", "Роль"})
	for _, t := range r.Teams {
		for _, m := range t.Members {
			role := ""
			if m.Role != nil {
				role = *m.Role
			}
			_ = w.Write(safeRow(t.Name, m.User.LastName, m.User.FirstName, deref(m.User.Link), role))
		}
	}
	for _, m := range r.Waitlist {
		_ = w.Write(safeRow("", m.User.LastName, m.User.FirstName, deref(m.User.Link), ""))
	}
	w.Flush()
	return buf.Bytes()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// safeRow защищает от CSV-инъекции: ячейка, начинающаяся с = + - @ или
// управляющего символа, исполнилась бы в Excel как формула. Имена и контакты
// вводят пользователи, поэтому такие значения получают ведущий апостроф.
func safeRow(cells ...string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		if strings.ContainsAny(c[:min(1, len(c))], "=+-@\t\r") {
			c = "'" + c
		}
		out[i] = c
	}
	return out
}
