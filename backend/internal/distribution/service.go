package distribution

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"slices"
)

// Store — то, что сервису нужно от репозитория; операции работают в
// транзакции из ctx, если она там есть.
type Store interface {
	InsertRun(ctx context.Context, r Run) (Run, error)
	GetRun(ctx context.Context, id, splitID int64) (Run, error) // ErrRunNotFound
	GetPublishedRun(ctx context.Context, splitID int64) (Run, error)
	ListRuns(ctx context.Context, splitID int64) ([]Run, error)
	MarkPublished(ctx context.Context, id int64, teamIDs []int64) (Run, error)
}

type Service struct {
	store  Store
	tx     TxManager
	splits SplitGate
	roster Roster
	teams  TeamWriter
	users  UserDirectory
	events EventPublisher
	// newSeed подменяется в тестах
	newSeed func() (int64, error)
}

func NewService(store Store, tx TxManager, splits SplitGate, roster Roster, teams TeamWriter,
	users UserDirectory, events EventPublisher) *Service {
	return &Service{store: store, tx: tx, splits: splits, roster: roster, teams: teams,
		users: users, events: events, newSeed: randomSeed}
}

// SetSeedGenerator подменяет источник seed в тестах.
func (s *Service) SetSeedGenerator(f func() (int64, error)) { s.newSeed = f }

// maxSeed — 2^53: больше JavaScript не читает без потери точности, а фронтенд
// получает seed из ответа и отправляет обратно.
const maxSeed = 1 << 53

func randomSeed() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("random seed: %w", err)
	}
	return int64(binary.BigEndian.Uint64(b[:]) % maxSeed), nil
}

// Preview делает прогон и сохраняет его, не публикуя: участники ничего не
// видят, статус не меняется.
func (s *Service) Preview(ctx context.Context, userID, splitID int64, seed *int64) (RunView, error) {
	st, err := s.requireOwner(ctx, userID, splitID)
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Preview: %w", err)
	}
	if err := checkRunnable(st); err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Preview: %w", err)
	}
	cands, err := s.roster.Candidates(ctx, splitID)
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Preview: candidates: %w", err)
	}
	run, err := s.compute(st.Rules, splitID, cands, seed)
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Preview: %w", err)
	}
	saved, err := s.store.InsertRun(ctx, run)
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Preview: %w", err)
	}
	return s.view(ctx, saved, cands)
}

// Publish фиксирует результат. Вся запись идёт одной транзакцией под
// advisory-lock участников и FOR UPDATE на пространстве; статус проверяется
// уже под блокировкой, поэтому одновременные вызовы создают команды один раз,
// а остальные получают тот же опубликованный прогон.
func (s *Service) Publish(ctx context.Context, userID, splitID int64, runID *int64) (RunView, error) {
	// права проверяются до транзакции, чтобы посторонний не вставал в очередь за lock
	if _, err := s.requireOwner(ctx, userID, splitID); err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Publish: %w", err)
	}

	var (
		run   Run
		cands []Candidate
		fresh bool
	)
	err := s.tx.Run(ctx, func(ctx context.Context) error {
		if err := s.roster.Lock(ctx, splitID); err != nil {
			return fmt.Errorf("lock roster: %w", err)
		}
		st, err := s.splits.LockForPublish(ctx, splitID)
		if err != nil {
			return fmt.Errorf("lock split: %w", err)
		}
		cands, err = s.roster.Candidates(ctx, splitID)
		if err != nil {
			return fmt.Errorf("candidates: %w", err)
		}
		if st.Status == StatusFinished {
			run, err = s.store.GetPublishedRun(ctx, splitID)
			if err != nil {
				return fmt.Errorf("published run: %w", err)
			}
			return nil
		}
		if err := checkRunnable(st); err != nil {
			return err
		}

		if runID != nil {
			run, err = s.store.GetRun(ctx, *runID, splitID)
			if err != nil {
				return err
			}
			if err := checkFresh(st.Rules, run, cands); err != nil {
				return err
			}
		} else {
			run, err = s.compute(st.Rules, splitID, cands, nil)
			if err != nil {
				return err
			}
			if run, err = s.store.InsertRun(ctx, run); err != nil {
				return err
			}
		}

		names := make([]string, len(run.Result.Teams))
		for i, t := range run.Result.Teams {
			names[i] = t.Name
		}
		teamIDs, err := s.teams.CreateTeams(ctx, splitID, names)
		if err != nil {
			return fmt.Errorf("create teams: %w", err)
		}
		if len(teamIDs) != len(names) {
			return fmt.Errorf("create teams: got %d ids for %d teams", len(teamIDs), len(names))
		}
		var placements []Placement
		for i, t := range run.Result.Teams {
			for _, m := range t.Members {
				placements = append(placements, Placement{MemberID: m.MemberID, TeamID: teamIDs[i], Role: m.Role})
			}
		}
		if err := s.roster.Assign(ctx, splitID, placements); err != nil {
			return fmt.Errorf("assign members: %w", err)
		}
		if run, err = s.store.MarkPublished(ctx, run.ID, teamIDs); err != nil {
			return err
		}
		if err := s.splits.MarkFinished(ctx, splitID); err != nil {
			return fmt.Errorf("finish split: %w", err)
		}
		fresh = true
		return nil
	})
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service.Publish: %w", err)
	}

	// события и чтение пользователей — только после коммита
	if fresh {
		s.events.Published(splitID, run.ID)
	}
	return s.view(ctx, run, cands)
}

// ListRuns — история прогонов без составов, новые сверху.
func (s *Service) ListRuns(ctx context.Context, userID, splitID int64) ([]Run, error) {
	if _, err := s.requireOwner(ctx, userID, splitID); err != nil {
		return nil, fmt.Errorf("distribution.Service.ListRuns: %w", err)
	}
	runs, err := s.store.ListRuns(ctx, splitID)
	if err != nil {
		return nil, fmt.Errorf("distribution.Service.ListRuns: %w", err)
	}
	return runs, nil
}

// compute выполняет алгоритм и проверяет результат независимым Validate:
// нарушение — баг алгоритма, а не ответ клиенту.
func (s *Service) compute(rules Rules, splitID int64, cands []Candidate, seed *int64) (Run, error) {
	var sd int64
	if seed != nil {
		sd = *seed
	} else {
		var err error
		if sd, err = s.newSeed(); err != nil {
			return Run{}, err
		}
	}
	res, err := Distribute(rules, cands, sd)
	if err != nil {
		return Run{}, err
	}
	if v := Validate(rules, cands, res); len(v) > 0 {
		return Run{}, fmt.Errorf("algorithm %s broke invariants (seed %d): %v", AlgorithmGreedyV1, sd, v)
	}
	return Run{
		SplitID:   splitID,
		Algorithm: AlgorithmGreedyV1,
		Seed:      sd,
		Roster:    rosterIDs(cands),
		Result:    res,
		Metrics:   ComputeMetrics(rules, cands, res),
	}, nil
}

func checkRunnable(st SplitState) error {
	switch st.Status {
	case StatusFinished:
		return ErrAlreadyFinished
	case StatusDraft:
		return ErrSplitNotOpen
	}
	return nil
}

// checkFresh: прогон из preview публикуется, только если состав не менялся, а
// результат по-прежнему удовлетворяет текущим правилам (их могли поправить).
func checkFresh(rules Rules, run Run, cands []Candidate) error {
	if !slices.Equal(run.Roster, rosterIDs(cands)) {
		return ErrRosterChanged
	}
	if v := Validate(rules, cands, run.Result); len(v) > 0 {
		return ErrRosterChanged
	}
	return nil
}

func rosterIDs(cands []Candidate) []int64 {
	ids := make([]int64, len(cands))
	for i, c := range cands {
		ids[i] = c.MemberID
	}
	slices.Sort(ids)
	return ids
}

// requireOwner: владелец — состояние пространства, участник — ErrForbidden,
// остальные — ErrSplitNotFound.
func (s *Service) requireOwner(ctx context.Context, userID, splitID int64) (SplitState, error) {
	st, err := s.splits.State(ctx, splitID)
	if err != nil {
		return SplitState{}, err
	}
	if st.OwnerID == userID {
		return st, nil
	}
	ok, err := s.roster.IsMember(ctx, splitID, userID)
	if err != nil {
		return SplitState{}, fmt.Errorf("check membership: %w", err)
	}
	if ok {
		return SplitState{}, ErrForbidden
	}
	return SplitState{}, ErrSplitNotFound
}

// view подтягивает публичные сведения об участниках одним запросом.
func (s *Service) view(ctx context.Context, run Run, cands []Candidate) (RunView, error) {
	userOf := make(map[int64]int64, len(cands))
	for _, c := range cands {
		userOf[c.MemberID] = c.UserID
	}
	var ids []int64
	for _, t := range run.Result.Teams {
		for _, m := range t.Members {
			ids = append(ids, userOf[m.MemberID])
		}
	}
	for _, m := range run.Result.Waitlist {
		ids = append(ids, userOf[m])
	}
	briefs, err := s.users.ListBriefs(ctx, ids)
	if err != nil {
		return RunView{}, fmt.Errorf("distribution.Service: list user briefs: %w", err)
	}
	byID := make(map[int64]UserBrief, len(briefs))
	for _, b := range briefs {
		byID[b.ID] = b
	}

	v := RunView{Run: run, Teams: make([]TeamView, len(run.Result.Teams)), Waitlist: []UserBrief{}}
	for i, t := range run.Result.Teams {
		tv := TeamView{Name: t.Name, Members: make([]MemberView, len(t.Members))}
		if i < len(run.TeamIDs) {
			id := run.TeamIDs[i]
			tv.ID = &id
		}
		for j, m := range t.Members {
			tv.Members[j] = MemberView{MemberID: m.MemberID, User: byID[userOf[m.MemberID]], Role: m.Role}
		}
		v.Teams[i] = tv
	}
	for _, m := range run.Result.Waitlist {
		v.Waitlist = append(v.Waitlist, byID[userOf[m]])
	}
	return v, nil
}
