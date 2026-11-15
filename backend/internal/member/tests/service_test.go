package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

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
