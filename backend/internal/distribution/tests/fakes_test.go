package tests

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"builder/backend/internal/distribution"
)

const (
	owner   int64 = 1
	student int64 = 2
	other   int64 = 3
	splitID int64 = 100
)

// callLog общий для фейков: по нему проверяется порядок обращений в транзакции.
type callLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *callLog) add(s string) {
	l.mu.Lock()
	l.calls = append(l.calls, s)
	l.mu.Unlock()
}

func (l *callLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (l *callLog) count(s string) int {
	n := 0
	for _, c := range l.snapshot() {
		if c == s {
			n++
		}
	}
	return n
}

// fakeTx сериализует транзакции, как FOR UPDATE, и помнит, что внутри неё идёт.
type fakeTx struct {
	mu     sync.Mutex
	active bool
}

func (f *fakeTx) Run(ctx context.Context, fn func(context.Context) error) error {
	f.mu.Lock()
	f.active = true
	defer func() { f.active = false; f.mu.Unlock() }()
	return fn(ctx)
}

type fakeGate struct {
	mu    sync.Mutex
	log   *callLog
	state distribution.SplitState
	err   error
}

func (g *fakeGate) get() distribution.SplitState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

func (g *fakeGate) setStatus(s string) {
	g.mu.Lock()
	g.state.Status = s
	g.mu.Unlock()
}

func (g *fakeGate) setRules(r distribution.Rules) {
	g.mu.Lock()
	g.state.Rules = r
	g.mu.Unlock()
}

func (g *fakeGate) State(context.Context, int64) (distribution.SplitState, error) {
	if g.err != nil {
		return distribution.SplitState{}, g.err
	}
	return g.get(), nil
}

func (g *fakeGate) LockForPublish(context.Context, int64) (distribution.SplitState, error) {
	g.log.add("split.LockForPublish")
	return g.get(), g.err
}

func (g *fakeGate) MarkFinished(context.Context, int64) error {
	g.log.add("split.MarkFinished")
	g.setStatus(distribution.StatusFinished)
	return nil
}

type fakeRoster struct {
	mu        sync.Mutex
	log       *callLog
	cands     []distribution.Candidate
	members   map[int64]bool // userID -> участник
	assigned  []distribution.Placement
	assignErr error
}

func (r *fakeRoster) set(c []distribution.Candidate) {
	r.mu.Lock()
	r.cands = c
	r.mu.Unlock()
}

func (r *fakeRoster) add(c distribution.Candidate) {
	r.mu.Lock()
	r.cands = append(r.cands, c)
	r.mu.Unlock()
}

func (r *fakeRoster) Lock(context.Context, int64) error { r.log.add("roster.Lock"); return nil }

func (r *fakeRoster) Candidates(context.Context, int64) ([]distribution.Candidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]distribution.Candidate(nil), r.cands...), nil
}

func (r *fakeRoster) IsMember(_ context.Context, _, userID int64) (bool, error) {
	return r.members[userID], nil
}

func (r *fakeRoster) Assign(_ context.Context, _ int64, ps []distribution.Placement) error {
	r.log.add("roster.Assign")
	if r.assignErr != nil {
		return r.assignErr
	}
	r.mu.Lock()
	r.assigned = append(r.assigned, ps...)
	r.mu.Unlock()
	return nil
}

type fakeTeams struct{ log *callLog }

func (t fakeTeams) CreateTeams(_ context.Context, _ int64, names []string) ([]int64, error) {
	t.log.add("team.CreateTeams")
	ids := make([]int64, len(names))
	for i := range names {
		ids[i] = int64(500 + i)
	}
	return ids, nil
}

type fakeUsers struct {
	mu    sync.Mutex
	calls int
}

func (u *fakeUsers) ListBriefs(_ context.Context, ids []int64) ([]distribution.UserBrief, error) {
	u.mu.Lock()
	u.calls++
	u.mu.Unlock()
	out := make([]distribution.UserBrief, len(ids))
	for i, id := range ids {
		out[i] = distribution.UserBrief{ID: id, FirstName: "Имя", LastName: fmt.Sprintf("Фамилия%d", id)}
	}
	return out, nil
}

type fakeEvents struct {
	mu          sync.Mutex
	runs        []int64
	insideTx    bool
	tx          *fakeTx
	observedTx  bool
	observedSet bool
}

func (e *fakeEvents) Published(_ int64, runID int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs = append(e.runs, runID)
	// событие не должно уходить из транзакции: подписчик увидит откатываемое
	if e.tx.mu.TryLock() {
		e.tx.mu.Unlock()
	} else {
		e.insideTx = true
	}
}

type memStore struct {
	mu     sync.Mutex
	nextID int64
	runs   map[int64]distribution.Run
}

func newMemStore() *memStore { return &memStore{runs: map[int64]distribution.Run{}} }

func (m *memStore) InsertRun(_ context.Context, r distribution.Run) (distribution.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	r.ID = m.nextID
	r.CreatedAt = time.Unix(1700000000+m.nextID, 0).UTC()
	m.runs[r.ID] = r
	return r, nil
}

func (m *memStore) GetRun(_ context.Context, id, split int64) (distribution.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok || r.SplitID != split {
		return distribution.Run{}, distribution.ErrRunNotFound
	}
	return r, nil
}

func (m *memStore) GetPublishedRun(_ context.Context, split int64) (distribution.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.SplitID == split && r.Published() {
			return r, nil
		}
	}
	return distribution.Run{}, distribution.ErrRunNotFound
}

func (m *memStore) ListRuns(_ context.Context, split int64) ([]distribution.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []distribution.Run
	for _, r := range m.runs {
		if r.SplitID == split {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (m *memStore) MarkPublished(_ context.Context, id int64, teamIDs []int64) (distribution.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id]
	if r.Published() {
		return distribution.Run{}, fmt.Errorf("run %d is already published", id)
	}
	now := time.Unix(1800000000, 0).UTC()
	r.PublishedAt, r.TeamIDs = &now, teamIDs
	m.runs[id] = r
	return r, nil
}

type env struct {
	svc    *distribution.Service
	log    *callLog
	gate   *fakeGate
	roster *fakeRoster
	store  *memStore
	users  *fakeUsers
	events *fakeEvents
	tx     *fakeTx
}

func testRules(policy distribution.RemainderPolicy) distribution.Rules {
	return distribution.Rules{
		TeamSize: 3, Policy: policy,
		Roles: []distribution.Role{{Name: "Backend", Min: 1, Max: 2, SortOrder: 0}, {Name: "Frontend", Min: 1, Max: 2, SortOrder: 1}},
	}
}

// makeCandidates: n участников с id 10, 11, ...; нечётные хотят Backend, чётные Frontend.
func makeCandidates(n int) []distribution.Candidate {
	out := make([]distribution.Candidate, n)
	for i := range out {
		role := "Frontend"
		if i%2 == 1 {
			role = "Backend"
		}
		out[i] = distribution.Candidate{
			MemberID: int64(10 + i), UserID: int64(1000 + i),
			JoinedAt:    time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC),
			Experience:  i % 5,
			Preferences: []distribution.RolePreference{{Role: role, Rank: 1}},
		}
	}
	return out
}

func newEnv(policy distribution.RemainderPolicy, n int) *env {
	log := &callLog{}
	tx := &fakeTx{}
	e := &env{
		log:    log,
		tx:     tx,
		gate:   &fakeGate{log: log, state: distribution.SplitState{OwnerID: owner, Status: "Open", Rules: testRules(policy)}},
		roster: &fakeRoster{log: log, cands: makeCandidates(n), members: map[int64]bool{student: true}},
		store:  newMemStore(),
		users:  &fakeUsers{},
		events: &fakeEvents{tx: tx},
	}
	e.svc = distribution.NewService(e.store, tx, e.gate, e.roster, fakeTeams{log}, e.users, e.events)
	return e
}

func ptr[T any](v T) *T { return &v }
