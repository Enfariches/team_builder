package tests

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"builder/backend/internal/distribution"
)

func TestPreview(t *testing.T) {
	ctx := context.Background()

	t.Run("не публикует: ни команд, ни назначений, ни статуса", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		v, err := e.svc.Preview(ctx, owner, splitID, ptr(int64(42)))
		if err != nil {
			t.Fatal(err)
		}
		if v.Run.Published() || v.Run.Seed != 42 || len(v.Teams) != 2 || v.Run.Algorithm != distribution.AlgorithmGreedyV1 {
			t.Fatalf("run = %+v", v.Run)
		}
		for _, call := range []string{"roster.Lock", "team.CreateTeams", "roster.Assign", "split.MarkFinished"} {
			if e.log.count(call) != 0 {
				t.Errorf("preview вызвал %s", call)
			}
		}
		if e.gate.get().Status != "Open" || len(e.events.runs) != 0 {
			t.Error("preview изменил статус или отправил событие")
		}
		if v.Teams[0].ID != nil {
			t.Error("у непубликованного прогона не может быть id команды")
		}
	})

	t.Run("один seed — один результат, прогоны сохраняются", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		a, _ := e.svc.Preview(ctx, owner, splitID, ptr(int64(7)))
		b, _ := e.svc.Preview(ctx, owner, splitID, ptr(int64(7)))
		if a.Run.ID == b.Run.ID {
			t.Fatal("прогоны не различаются по id")
		}
		if !reflect.DeepEqual(a.Run.Result, b.Run.Result) {
			t.Fatal("тот же seed на том же составе дал разный результат")
		}
	})

	tests := []struct {
		name   string
		user   int64
		status string
		n      int
		want   error
	}{
		{"Closed допустим", owner, "Closed", 8, nil},
		{"Draft", owner, "Draft", 8, distribution.ErrSplitNotOpen},
		{"Finished", owner, "Finished", 8, distribution.ErrAlreadyFinished},
		{"участник получает 403", student, "Open", 8, distribution.ErrForbidden},
		{"посторонний получает 404", other, "Open", 8, distribution.ErrSplitNotFound},
		{"мало участников", owner, "Open", 2, distribution.ErrNotEnoughMembers},
		{"никого", owner, "Open", 0, distribution.ErrNotEnoughMembers},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(distribution.PolicySpread, tc.n)
			e.gate.setStatus(tc.status)
			_, err := e.svc.Preview(ctx, tc.user, splitID, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil && len(e.store.runs) != 0 {
				t.Error("прогон сохранён несмотря на ошибку")
			}
		})
	}

}

func TestPublish(t *testing.T) {
	ctx := context.Background()

	t.Run("без runId: один транзакционный путь в правильном порядке", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		v, err := e.svc.Publish(ctx, owner, splitID, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"roster.Lock", "split.LockForPublish", "team.CreateTeams", "roster.Assign", "split.MarkFinished"}
		if got := e.log.snapshot(); !reflect.DeepEqual(got, want) {
			t.Fatalf("порядок обращений = %v, want %v", got, want)
		}
		if !v.Run.Published() || len(v.Run.TeamIDs) != len(v.Teams) || *v.Teams[0].ID != 500 {
			t.Fatalf("run = %+v teams = %+v", v.Run, v.Teams)
		}
		if e.gate.get().Status != distribution.StatusFinished {
			t.Error("пространство не Finished")
		}
		if len(e.roster.assigned) != 8 {
			t.Errorf("назначено %d из 8", len(e.roster.assigned))
		}
		if len(e.events.runs) != 1 || e.events.runs[0] != v.Run.ID || e.events.insideTx {
			t.Errorf("события = %v (внутри транзакции: %v)", e.events.runs, e.events.insideTx)
		}
	})

	t.Run("с runId публикуется именно показанный прогон", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		pv, _ := e.svc.Preview(ctx, owner, splitID, ptr(int64(5)))
		v, err := e.svc.Publish(ctx, owner, splitID, &pv.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Run.ID != pv.Run.ID || v.Run.Seed != 5 || len(e.store.runs) != 1 {
			t.Fatalf("опубликован другой прогон: %+v, прогонов в хранилище %d", v.Run, len(e.store.runs))
		}
		if !reflect.DeepEqual(v.Run.Result, pv.Run.Result) {
			t.Error("состав команд отличается от показанного в preview")
		}
	})

	t.Run("состав изменился после preview — roster_changed, ничего не записано", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		pv, _ := e.svc.Preview(ctx, owner, splitID, nil)
		e.roster.add(makeCandidates(9)[8])
		_, err := e.svc.Publish(ctx, owner, splitID, &pv.Run.ID)
		if !errors.Is(err, distribution.ErrRosterChanged) {
			t.Fatalf("err = %v", err)
		}
		if e.log.count("team.CreateTeams") != 0 || e.log.count("split.MarkFinished") != 0 || len(e.events.runs) != 0 {
			t.Error("публикация оставила следы после отказа")
		}
	})

	t.Run("правила поменяли после preview — roster_changed", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		pv, _ := e.svc.Preview(ctx, owner, splitID, nil)
		// старый прогон даёт по 2 backend в команде, новые правила требуют ровно 3
		r := testRules(distribution.PolicySpread)
		r.TeamSize = 4
		r.Roles[0].Min, r.Roles[0].Max = 3, 3
		r.Roles[1].Min, r.Roles[1].Max = 1, 2
		e.gate.setRules(r)
		if _, err := e.svc.Publish(ctx, owner, splitID, &pv.Run.ID); !errors.Is(err, distribution.ErrRosterChanged) {
			t.Fatalf("err = %v: прогон под старые квоты опубликован", err)
		}
	})

	t.Run("повторный publish возвращает тот же результат и ничего не пишет", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 8)
		first, err := e.svc.Publish(ctx, owner, splitID, nil)
		if err != nil {
			t.Fatal(err)
		}
		again, err := e.svc.Publish(ctx, owner, splitID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if again.Run.ID != first.Run.ID || !reflect.DeepEqual(first.Teams, again.Teams) {
			t.Fatal("повтор вернул другой результат")
		}
		if e.log.count("team.CreateTeams") != 1 || len(e.events.runs) != 1 || len(e.store.runs) != 1 {
			t.Fatalf("повтор записал: команд %d, событий %d, прогонов %d",
				e.log.count("team.CreateTeams"), len(e.events.runs), len(e.store.runs))
		}
		// повтор с runId, пока всё опубликовано, тоже идемпотентен
		withID, err := e.svc.Publish(ctx, owner, splitID, &first.Run.ID)
		if err != nil || withID.Run.ID != first.Run.ID {
			t.Fatalf("повтор с runId: %v", err)
		}
	})

	t.Run("одновременные publish создают один набор команд", func(t *testing.T) {
		e := newEnv(distribution.PolicySpread, 12)
		const callers = 10
		var (
			wg  sync.WaitGroup
			mu  sync.Mutex
			ids = map[int64]bool{}
		)
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := e.svc.Publish(ctx, owner, splitID, nil)
				if err != nil {
					t.Errorf("publish: %v", err)
					return
				}
				mu.Lock()
				ids[v.Run.ID] = true
				mu.Unlock()
			}()
		}
		wg.Wait()
		if len(ids) != 1 || e.log.count("team.CreateTeams") != 1 || len(e.events.runs) != 1 || len(e.store.runs) != 1 {
			t.Fatalf("прогонов в ответах %d, команд создано %d раз, событий %d, прогонов в хранилище %d",
				len(ids), e.log.count("team.CreateTeams"), len(e.events.runs), len(e.store.runs))
		}
	})

	tests := []struct {
		name   string
		user   int64
		status string
		n      int
		want   error
	}{
		{"Draft", owner, "Draft", 8, distribution.ErrSplitNotOpen},
		{"Closed допустим", owner, "Closed", 8, nil},
		{"участник получает 403", student, "Open", 8, distribution.ErrForbidden},
		{"посторонний получает 404", other, "Open", 8, distribution.ErrSplitNotFound},
		{"мало участников", owner, "Open", 2, distribution.ErrNotEnoughMembers},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(distribution.PolicySpread, tc.n)
			e.gate.setStatus(tc.status)
			_, err := e.svc.Publish(ctx, tc.user, splitID, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil && (e.log.count("team.CreateTeams") != 0 || len(e.events.runs) != 0) {
				t.Error("отказ оставил следы")
			}
		})
	}

}
