package tests

import (
	"context"
	"errors"
	"sync"
	"time"

	"builder/backend/internal/split"
)

// fakeStore — рукописный двойник Store и TxRunner в одном.
type fakeStore struct {
	mu       sync.Mutex
	splits   map[int64]split.Split
	nextID   int64
	failNext error // ошибка, которую вернёт следующий вызов Run после fn
}

func newFakeStore() *fakeStore { return &fakeStore{splits: map[int64]split.Split{}} }

func (f *fakeStore) Run(_ context.Context, fn func(split.Store) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := fn(f); err != nil {
		return err
	}
	if err := f.failNext; err != nil {
		f.failNext = nil
		return err
	}
	return nil
}

func (f *fakeStore) Insert(_ context.Context, n split.NewSplit) (split.Split, error) {
	f.nextID++
	sp := split.Split{
		ID: f.nextID, OwnerID: n.OwnerID, Title: n.Title, Description: n.Description,
		Status: split.StatusDraft, TeamSize: n.TeamSize, MaxMembers: n.MaxMembers,
		RemainderPolicy: n.RemainderPolicy, AutoFinishAt: n.AutoFinishAt,
		Roles: toRoles(n.Roles), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	f.splits[sp.ID] = sp
	return sp, nil
}

func (f *fakeStore) Get(_ context.Context, id int64) (split.Split, error) {
	sp, ok := f.splits[id]
	if !ok {
		return split.Split{}, split.ErrNotFound
	}
	return sp, nil
}

func (f *fakeStore) GetForUpdate(ctx context.Context, id int64) (split.Split, error) {
	return f.Get(ctx, id)
}

func (f *fakeStore) Update(_ context.Context, id int64, p split.Patch) (split.Split, error) {
	sp, ok := f.splits[id]
	if !ok {
		return split.Split{}, split.ErrNotFound
	}
	if p.Title != nil {
		sp.Title = *p.Title
	}
	if p.TeamSize != nil {
		sp.TeamSize = *p.TeamSize
	}
	if p.Description.Set {
		sp.Description = p.Description.Value
	}
	if p.MaxMembers.Set {
		sp.MaxMembers = p.MaxMembers.Value
	}
	if p.AutoFinishAt.Set {
		sp.AutoFinishAt = p.AutoFinishAt.Value
	}
	if p.RemainderPolicy != nil {
		sp.RemainderPolicy = *p.RemainderPolicy
	}
	if p.Roles != nil {
		sp.Roles = toRoles(*p.Roles)
	}
	f.splits[id] = sp
	return sp, nil
}

func (f *fakeStore) SetStatus(_ context.Context, id int64, st split.Status) (split.Split, error) {
	sp := f.splits[id]
	sp.Status = st
	f.splits[id] = sp
	return sp, nil
}

func (f *fakeStore) Delete(_ context.Context, id int64) error {
	delete(f.splits, id)
	return nil
}

func (f *fakeStore) ListForUser(_ context.Context, userID int64, memberIDs []int64, role *string, status *split.Status) ([]split.Split, error) {
	isMember := map[int64]bool{}
	for _, id := range memberIDs {
		isMember[id] = true
	}
	var out []split.Split
	for id := int64(1); id <= f.nextID; id++ {
		sp, ok := f.splits[id]
		if !ok || (sp.OwnerID != userID && !isMember[id]) {
			continue
		}
		if role != nil && ((*role == "owner") != (sp.OwnerID == userID)) {
			continue
		}
		if status != nil && sp.Status != *status {
			continue
		}
		out = append(out, sp)
	}
	return out, nil
}

func (f *fakeStore) setStatus(id int64, st split.Status) {
	sp := f.splits[id]
	sp.Status = st
	f.splits[id] = sp
}

func toRoles(in []split.RoleInput) []split.Role {
	out := make([]split.Role, len(in))
	for i, r := range in {
		out[i] = split.Role{ID: int64(i + 1), Name: r.Name, PerTeamMin: r.PerTeamMin, PerTeamMax: r.PerTeamMax, SortOrder: i}
	}
	return out
}

// fakeMembers реализует оба порта: членство и статистику.
type fakeMembers struct {
	members map[[2]int64]bool // {splitID, userID}
	counts  map[int64]int
}

func newFakeMembers() *fakeMembers {
	return &fakeMembers{members: map[[2]int64]bool{}, counts: map[int64]int{}}
}

func (m *fakeMembers) IsMember(_ context.Context, splitID, userID int64) (bool, error) {
	return m.members[[2]int64{splitID, userID}], nil
}

func (m *fakeMembers) SplitIDsOfUser(_ context.Context, userID int64) ([]int64, error) {
	var ids []int64
	for k, v := range m.members {
		if v && k[1] == userID {
			ids = append(ids, k[0])
		}
	}
	return ids, nil
}

func (m *fakeMembers) Stats(_ context.Context, ids []int64) (map[int64]split.Stats, error) {
	out := map[int64]split.Stats{}
	for _, id := range ids {
		out[id] = split.Stats{MembersCount: m.counts[id]}
	}
	return out, nil
}

func (m *fakeMembers) add(splitID, userID int64) {
	m.members[[2]int64{splitID, userID}] = true
	m.counts[splitID]++
}

var errBoom = errors.New("boom")

const (
	owner    int64 = 1
	member   int64 = 2
	stranger int64 = 3
)

var validNew = split.NewSplit{
	Title: "Практикум", TeamSize: 5, Roles: roles([2]int{1, 2}, [2]int{1, 2}, [2]int{1, 1}, [2]int{0, 1}),
}

// newEnv создаёт сервис и пространство owner-а в статусе Draft; member уже участник.
func newEnv(t interface{ Fatal(...any) }) (*split.Service, *fakeStore, *fakeMembers, *split.Hub, int64) {
	st, mem, hub := newFakeStore(), newFakeMembers(), split.NewHub()
	svc := split.NewService(st, st, mem, mem, hub)
	sp, err := svc.Create(context.Background(), owner, validNew)
	if err != nil {
		t.Fatal(err)
	}
	mem.add(sp.ID, member)
	return svc, st, mem, hub, sp.ID
}
