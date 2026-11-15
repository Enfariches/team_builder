package tests

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"builder/backend/internal/member"
)

const (
	owner   int64 = 1
	student int64 = 2
	other   int64 = 3
	splitID int64 = 100
)

// memStore — хранилище в памяти; сам же служит TxRunner (транзакции без отката
// достаточно для юнит-тестов логики сервиса).
type memStore struct {
	mu      sync.Mutex
	nextID  int64
	members map[int64]member.Member
	locks   []int64
}

func newMemStore() *memStore { return &memStore{members: map[int64]member.Member{}} }

func (m *memStore) Run(_ context.Context, fn func(member.Store) error) error { return fn(m) }

func (m *memStore) LockSplit(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.locks = append(m.locks, id)
	return nil
}

func (m *memStore) Insert(_ context.Context, n member.NewMember) (member.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.members {
		if x.SplitID == n.SplitID && x.UserID == n.UserID {
			return member.Member{}, member.ErrAlreadyMember
		}
	}
	m.nextID++
	fid := n.FormID
	mm := member.Member{ID: m.nextID, SplitID: n.SplitID, UserID: n.UserID, FormID: &fid,
		Snapshot: n.Snapshot, JoinedAt: time.Unix(1700000000, 0).UTC()}
	m.members[mm.ID] = mm
	return mm, nil
}

func (m *memStore) Get(_ context.Context, id int64) (member.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mm, ok := m.members[id]
	if !ok {
		return member.Member{}, member.ErrNotFound
	}
	return mm, nil
}

func (m *memStore) GetByUser(_ context.Context, split, user int64) (member.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.members {
		if x.SplitID == split && x.UserID == user {
			return x, nil
		}
	}
	return member.Member{}, member.ErrNotFound
}

func (m *memStore) List(_ context.Context, split int64) ([]member.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []member.Member
	for _, x := range m.members {
		if x.SplitID == split {
			out = append(out, x)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memStore) Count(ctx context.Context, split int64) (int, error) {
	l, _ := m.List(ctx, split)
	return len(l), nil
}

func (m *memStore) Counts(ctx context.Context, ids []int64) (map[int64]int, error) {
	out := map[int64]int{}
	for _, id := range ids {
		if n, _ := m.Count(ctx, id); n > 0 {
			out[id] = n
		}
	}
	return out, nil
}

func (m *memStore) SplitIDsOfUser(_ context.Context, user int64) ([]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []int64
	for _, x := range m.members {
		if x.UserID == user {
			out = append(out, x.SplitID)
		}
	}
	return out, nil
}

func (m *memStore) IsMember(ctx context.Context, split, user int64) (bool, error) {
	_, err := m.GetByUser(ctx, split, user)
	return err == nil, nil
}

func (m *memStore) Delete(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.members[id]; !ok {
		return member.ErrNotFound
	}
	delete(m.members, id)
	return nil
}

func (m *memStore) Assign(_ context.Context, split int64, ps []member.Placement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range ps {
		mm, ok := m.members[p.MemberID]
		if !ok || mm.SplitID != split {
			return member.ErrNotFound
		}
		team, role := p.TeamID, p.Role
		mm.TeamID, mm.AssignedRole = &team, &role
		m.members[p.MemberID] = mm
	}
	return nil
}

func (m *memStore) ListAssignments(_ context.Context, split int64) ([]member.Assignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []member.Assignment
	for _, x := range m.members {
		if x.SplitID == split {
			out = append(out, member.Assignment{MemberID: x.ID, UserID: x.UserID, TeamID: x.TeamID, Role: x.AssignedRole})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberID < out[j].MemberID })
	return out, nil
}

type fakeInvites struct {
	mu        sync.Mutex
	redeemErr error
	releaseOf []int64
	redeemed  int
	splitID   int64
}

func (f *fakeInvites) Redeem(context.Context, string) (member.Redemption, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.redeemErr != nil {
		return member.Redemption{}, f.redeemErr
	}
	f.redeemed++
	return member.Redemption{InviteID: 7, SplitID: f.splitID}, nil
}

func (f *fakeInvites) Release(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseOf = append(f.releaseOf, id)
	return nil
}

type fakeForms struct {
	snap member.FormSnapshot
	err  error
}

func (f fakeForms) Snapshot(context.Context, int64, int64) (member.FormSnapshot, error) {
	return f.snap, f.err
}

type fakeSplits struct {
	mu   sync.Mutex
	info member.SplitInfo
	err  error
	// gate, если задан, останавливает чтение внутри транзакции: тест видит
	// момент входа через entered и сам решает, когда продолжить.
	gate    chan struct{}
	entered chan struct{}
}

func (f *fakeSplits) SplitInfo(context.Context, int64) (member.SplitInfo, error) {
	if f.gate != nil {
		f.entered <- struct{}{}
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.info, f.err
}

type fakeUsers struct{ calls atomic.Int32 }

func (f *fakeUsers) ListBriefs(_ context.Context, ids []int64) ([]member.UserBrief, error) {
	f.calls.Add(1)
	out := make([]member.UserBrief, len(ids))
	for i, id := range ids {
		out[i] = member.UserBrief{ID: id, FirstName: "Имя", LastName: "Фамилия"}
	}
	return out, nil
}

type fakeEvents struct {
	mu     sync.Mutex
	counts []int
}

func (f *fakeEvents) MembersChanged(_ int64, n int) {
	f.mu.Lock()
	f.counts = append(f.counts, n)
	f.mu.Unlock()
}

type env struct {
	svc     *member.Service
	store   *memStore
	invites *fakeInvites
	forms   *fakeForms
	splits  *fakeSplits
	users   *fakeUsers
	events  *fakeEvents
}

func openSplit(max *int) member.SplitInfo {
	return member.SplitInfo{OwnerID: owner, Status: member.StatusOpen, MaxMembers: max}
}

func newEnv(info member.SplitInfo) *env {
	e := &env{
		store:   newMemStore(),
		invites: &fakeInvites{splitID: splitID},
		forms:   &fakeForms{snap: member.FormSnapshot{Experience: 3, Skills: []string{"go"}}},
		splits:  &fakeSplits{info: info},
		users:   &fakeUsers{},
		events:  &fakeEvents{},
	}
	e.svc = member.NewService(e.store, e.store, e.invites, e.forms, e.splits, e.users, e.events)
	return e
}

func ptr[T any](v T) *T { return &v }
