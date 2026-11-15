package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"builder/backend/internal/member"
)

func TestJoin(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("boom")

	tests := []struct {
		name        string
		info        member.SplitInfo
		prepare     func(e *env)
		user        int64
		wantErr     error
		wantRelease bool // использование инвайта должно вернуться
		wantRedeem  bool // использование вообще занималось
	}{
		{name: "успех", info: openSplit(ptr(5)), user: student, wantRedeem: true},
		{name: "чужая анкета не тратит инвайт", info: openSplit(nil), user: student,
			prepare: func(e *env) { e.forms.err = member.ErrFormNotFound }, wantErr: member.ErrFormNotFound},
		{name: "плохой инвайт", info: openSplit(nil), user: student,
			prepare: func(e *env) { e.invites.redeemErr = member.ErrInviteInvalid }, wantErr: member.ErrInviteInvalid},
		{name: "повтор возвращает инвайт", info: openSplit(nil), user: student,
			prepare: func(e *env) {
				_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
			},
			wantErr: member.ErrAlreadyMember, wantRedeem: true, wantRelease: true},
		{name: "владелец", info: openSplit(nil), user: owner,
			wantErr: member.ErrAlreadyMember, wantRedeem: true, wantRelease: true},
		{name: "набор закрыт", info: member.SplitInfo{OwnerID: owner, Status: "Closed"}, user: student,
			wantErr: member.ErrSplitNotOpen, wantRedeem: true, wantRelease: true},
		{name: "мест нет", info: openSplit(ptr(1)), user: student,
			prepare: func(e *env) {
				_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: other, FormID: 1})
			},
			wantErr: member.ErrSplitFull, wantRedeem: true, wantRelease: true},
		{name: "пространства нет", info: openSplit(nil), user: student,
			prepare: func(e *env) { e.splits.err = member.ErrSplitNotFound },
			wantErr: member.ErrSplitNotFound, wantRedeem: true, wantRelease: true},
		{name: "сбой чтения пространства", info: openSplit(nil), user: student,
			prepare: func(e *env) { e.splits.err = errBoom },
			wantErr: errBoom, wantRedeem: true, wantRelease: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(tc.info)
			if tc.prepare != nil {
				tc.prepare(e)
			}
			before := len(e.events.counts)

			m, err := e.svc.Join(ctx, tc.user, "tok", 11)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got := e.invites.redeemed > 0; got != tc.wantRedeem {
				t.Errorf("redeem = %v, want %v", got, tc.wantRedeem)
			}
			if got := len(e.invites.releaseOf) > 0; got != tc.wantRelease {
				t.Errorf("release = %v, want %v", got, tc.wantRelease)
			}
			if tc.wantErr != nil {
				if len(e.events.counts) != before {
					t.Error("событие опубликовано при ошибке")
				}
				return
			}
			if m.UserID != tc.user || m.SplitID != splitID || m.FormID == nil || *m.FormID != 11 {
				t.Errorf("member = %+v", m)
			}
			if m.User.ID != tc.user {
				t.Errorf("UserBrief не проставлен: %+v", m.User)
			}
			if m.Snapshot.Experience != 3 || len(m.Snapshot.Skills) != 1 {
				t.Errorf("снимок анкеты не сохранён: %+v", m.Snapshot)
			}
			if len(e.events.counts) != before+1 || e.events.counts[before] != 1 {
				t.Errorf("events = %v", e.events.counts)
			}
			if len(e.store.locks) == 0 {
				t.Error("вступление без lock пространства")
			}
		})
	}
}

// Параллельные вступления на последнее место: принят ровно один, остальным
// возвращается использование инвайта.
func TestJoinConcurrentRespectsLimit(t *testing.T) {
	const limit, attempts = 3, 40
	e := newEnv(openSplit(ptr(limit)))

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ok, ful int
	)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			_, err := e.svc.Join(context.Background(), uid, "tok", 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, member.ErrSplitFull):
				ful++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}(int64(100 + i))
	}
	wg.Wait()

	if ok != limit || ful != attempts-limit {
		t.Fatalf("принято %d, отказано %d", ok, ful)
	}
	if n, _ := e.store.Count(context.Background(), splitID); n != limit {
		t.Fatalf("в хранилище %d участников", n)
	}
	if got := len(e.invites.releaseOf); got != attempts-limit {
		t.Fatalf("возвращено %d использований, ждали %d", got, attempts-limit)
	}
}

// Ожидание очереди на пространство прерывается отменой запроса; отказавшему
// возвращается использование инвайта, а очередь после него остаётся рабочей.
func TestJoinWaitIsCancellable(t *testing.T) {
	e := newEnv(openSplit(nil))
	e.splits.gate = make(chan struct{})
	e.splits.entered = make(chan struct{}, 2)

	first := make(chan error, 1)
	go func() {
		_, err := e.svc.Join(context.Background(), student, "tok", 1)
		first <- err
	}()
	<-e.splits.entered // первый внутри транзакции и держит пространство

	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		_, err := e.svc.Join(ctx, other, "tok", 1)
		second <- err
	}()
	time.Sleep(20 * time.Millisecond) // второй должен встать в очередь
	cancel()

	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("второй: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ожидание не прервалось отменой контекста")
	}
	if len(e.invites.releaseOf) != 1 {
		t.Fatalf("использование инвайта не возвращено: %v", e.invites.releaseOf)
	}

	close(e.splits.gate)
	if err := <-first; err != nil {
		t.Fatalf("первый: %v", err)
	}
	if n, _ := e.store.Count(context.Background(), splitID); n != 1 {
		t.Fatalf("участников %d, ждали 1", n)
	}
}

func TestListBySplitAccess(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		user int64
		want error
	}{
		{"владелец", owner, nil},
		{"участник получает 403", student, member.ErrForbidden},
		{"посторонний получает 404", other, member.ErrSplitNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(openSplit(nil))
			_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1,
				Snapshot: member.FormSnapshot{Experience: 4}})
			list, err := e.svc.ListBySplit(ctx, tc.user, splitID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if list != nil {
					t.Fatal("анкеты утекли вместе с ошибкой")
				}
				return
			}
			if len(list) != 1 || list[0].Snapshot.Experience != 4 || list[0].User.ID != student {
				t.Fatalf("list = %+v", list)
			}
			if n := e.users.calls.Load(); n != 1 {
				t.Errorf("ListBriefs вызван %d раз, ждали один запрос на список", n)
			}
		})
	}
}

func TestGetMine(t *testing.T) {
	ctx := context.Background()
	e := newEnv(openSplit(nil))
	if _, err := e.svc.GetMine(ctx, student, splitID); !errors.Is(err, member.ErrNotFound) {
		t.Fatalf("не участник: %v", err)
	}
	_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
	m, err := e.svc.GetMine(ctx, student, splitID)
	if err != nil || m.UserID != student || m.User.ID != student {
		t.Fatalf("m=%+v err=%v", m, err)
	}
}

func TestLeave(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		status string
		member bool
		want   error
	}{
		{"выход из открытого", member.StatusOpen, true, nil},
		{"выход при закрытом наборе", "Closed", true, nil},
		{"после публикации нельзя", member.StatusFinished, true, member.ErrSplitFinished},
		{"не участник", member.StatusOpen, false, member.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(member.SplitInfo{OwnerID: owner, Status: tc.status})
			if tc.member {
				_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
				_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: other, FormID: 1})
			}
			err := e.svc.Leave(ctx, student, splitID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			inSplit, _ := e.store.IsMember(ctx, splitID, student)
			if tc.want == nil {
				if inSplit {
					t.Error("участник не удалён")
				}
				if len(e.events.counts) != 1 || e.events.counts[0] != 1 {
					t.Errorf("events = %v, ждали [1]", e.events.counts)
				}
				if len(e.invites.releaseOf) != 0 {
					t.Error("использование инвайта не должно возвращаться при выходе")
				}
			} else if tc.member && !inSplit {
				t.Error("участник удалён несмотря на ошибку")
			}
		})
	}
}

func TestRemove(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		user   int64
		target int64 // id участника; 0 — существующий
		status string
		want   error
	}{
		{"владелец исключает", owner, 0, member.StatusOpen, nil},
		{"участник не может исключать (403)", other, 0, member.StatusOpen, member.ErrForbidden},
		{"посторонний видит 404", 999, 0, member.StatusOpen, member.ErrNotFound},
		{"участника нет", owner, 555, member.StatusOpen, member.ErrNotFound},
		{"после публикации нельзя", owner, 0, member.StatusFinished, member.ErrSplitFinished},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(member.SplitInfo{OwnerID: owner, Status: tc.status})
			victim, _ := e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
			_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: other, FormID: 1})
			id := tc.target
			if id == 0 {
				id = victim.ID
			}
			err := e.svc.Remove(ctx, tc.user, id)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			gone := func() bool { ok, _ := e.store.IsMember(ctx, splitID, student); return !ok }()
			if (tc.want == nil) != gone {
				t.Errorf("удалён = %v при ошибке %v", gone, err)
			}
			if tc.want == nil && (len(e.events.counts) != 1 || e.events.counts[0] != 1) {
				t.Errorf("events = %v", e.events.counts)
			}
		})
	}
}

// Адаптеры split получают данные через Service.
func TestSplitFacingQueries(t *testing.T) {
	ctx := context.Background()
	e := newEnv(openSplit(nil))
	_, _ = e.store.Insert(ctx, member.NewMember{SplitID: splitID, UserID: student, FormID: 1})
	_, _ = e.store.Insert(ctx, member.NewMember{SplitID: 200, UserID: student, FormID: 1})
	_, _ = e.store.Insert(ctx, member.NewMember{SplitID: 200, UserID: other, FormID: 1})

	if ok, _ := e.svc.IsMember(ctx, splitID, student); !ok {
		t.Error("IsMember = false")
	}
	if ok, _ := e.svc.IsMember(ctx, splitID, other); ok {
		t.Error("IsMember = true для не участника")
	}
	ids, _ := e.svc.SplitIDsOfUser(ctx, student)
	if len(ids) != 2 {
		t.Errorf("SplitIDsOfUser = %v", ids)
	}
	counts, _ := e.svc.Counts(ctx, []int64{splitID, 200, 300})
	if counts[splitID] != 1 || counts[200] != 2 || counts[300] != 0 {
		t.Errorf("Counts = %v", counts)
	}
}
