package tests

import (
	"context"

	"builder/backend/internal/team"
)

const (
	owner   int64 = 1
	alice   int64 = 2 // в команде 1
	bob     int64 = 3 // в команде 1
	carol   int64 = 4 // в команде 2
	dave    int64 = 5 // лист ожидания
	outside int64 = 9
	splitID int64 = 100
)

type memStore struct{ teams []team.Team }

func (m *memStore) Create(context.Context, int64, []string) ([]int64, error) { return nil, nil }
func (m *memStore) Counts(context.Context, []int64) (map[int64]int, error)   { return nil, nil }

func (m *memStore) List(context.Context, int64) ([]team.Team, error) {
	return append([]team.Team(nil), m.teams...), nil
}

type fakeSplits struct{ state team.SplitState }

func (f fakeSplits) State(_ context.Context, id int64) (team.SplitState, error) {
	if id != splitID {
		return team.SplitState{}, team.ErrSplitNotFound
	}
	return f.state, nil
}

type fakeMembers struct{ assigns []team.Assignment }

func (f fakeMembers) ListAssignments(context.Context, int64) ([]team.Assignment, error) {
	return f.assigns, nil
}

func (f fakeMembers) IsMember(_ context.Context, _, userID int64) (bool, error) {
	for _, a := range f.assigns {
		if a.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

type fakeUsers struct{ briefs map[int64]team.UserBrief }

func (f fakeUsers) ListBriefs(_ context.Context, ids []int64) ([]team.UserBrief, error) {
	var out []team.UserBrief
	for _, id := range ids {
		if b, ok := f.briefs[id]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

func ptr[T any](v T) *T { return &v }

func newService(status string) *team.Service {
	st := &memStore{teams: []team.Team{
		{ID: 11, SplitID: splitID, Name: "Команда 1"},
		{ID: 12, SplitID: splitID, Name: "Команда 2"},
	}}
	members := fakeMembers{assigns: []team.Assignment{
		{MemberID: 1, UserID: alice, TeamID: ptr(int64(11)), Role: ptr("Backend")},
		{MemberID: 2, UserID: bob, TeamID: ptr(int64(11)), Role: ptr("Frontend")},
		{MemberID: 3, UserID: carol, TeamID: ptr(int64(12)), Role: ptr("Backend")},
		{MemberID: 4, UserID: dave},
	}}
	users := fakeUsers{briefs: map[int64]team.UserBrief{
		alice: {ID: alice, FirstName: "Алиса", LastName: "Иванова", Link: ptr("@alice")},
		bob:   {ID: bob, FirstName: "Боб", LastName: "Петров", Link: ptr("bob@example.com")},
		carol: {ID: carol, FirstName: "=Кэрол", LastName: "Сидорова", Link: ptr(`есть, запятая и "кавычки"`)},
		dave:  {ID: dave, FirstName: "Дэйв", LastName: "Кузнецов"},
	}}
	return team.NewService(st, fakeSplits{team.SplitState{OwnerID: owner, Status: status}}, members, users)
}
