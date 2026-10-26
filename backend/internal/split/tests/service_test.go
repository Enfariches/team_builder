package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"builder/backend/internal/split"
)

func TestCreate(t *testing.T) {
	ctx := context.Background()

	t.Run("владелец, Draft, значения по умолчанию", func(t *testing.T) {
		svc, _, _, _, id := newEnv(t)
		sp, err := svc.Get(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if sp.MyRole != split.AccessOwner || sp.Status != split.StatusDraft || sp.RemainderPolicy != split.PolicySpread || sp.OwnerID != owner {
			t.Fatalf("%+v", sp)
		}
	})

	t.Run("невалидные правила не доходят до хранилища", func(t *testing.T) {
		st, mem := newFakeStore(), newFakeMembers()
		svc := split.NewService(st, st, mem, mem, split.NewHub())
		in := validNew
		in.TeamSize = 1
		_, err := svc.Create(ctx, owner, in)
		var ve *split.ValidationError
		if !errors.As(err, &ve) || !ve.Rules {
			t.Fatalf("err = %v", err)
		}
		if st.nextID != 0 {
			t.Fatal("пространство создано при невалидных правилах")
		}
	})
}

// Матрица доступа: владелец / участник / посторонний на каждой операции.
func TestAccessMatrix(t *testing.T) {
	ctx := context.Background()
	ops := []struct {
		name string
		call func(svc *split.Service, uid, id int64) error
		// ожидание для owner, member, stranger
		want [3]error
	}{
		{"Get", func(s *split.Service, uid, id int64) error { _, err := s.Get(ctx, uid, id); return err },
			[3]error{nil, nil, split.ErrNotFound}},
		{"Authorize", func(s *split.Service, uid, id int64) error { _, err := s.Authorize(ctx, uid, id); return err },
			[3]error{nil, nil, split.ErrNotFound}},
		{"Update", func(s *split.Service, uid, id int64) error {
			_, err := s.Update(ctx, uid, id, split.Patch{Title: ptr("Новое")})
			return err
		}, [3]error{nil, split.ErrForbidden, split.ErrNotFound}},
		{"ChangeStatus", func(s *split.Service, uid, id int64) error {
			_, err := s.ChangeStatus(ctx, uid, id, split.StatusOpen)
			return err
		}, [3]error{nil, split.ErrForbidden, split.ErrNotFound}},
		{"Delete", func(s *split.Service, uid, id int64) error { return s.Delete(ctx, uid, id) },
			[3]error{nil, split.ErrForbidden, split.ErrNotFound}},
	}
	for _, op := range ops {
		for i, uid := range []int64{owner, member, stranger} {
			t.Run(op.name+"/"+[]string{"owner", "member", "stranger"}[i], func(t *testing.T) {
				svc, _, _, _, id := newEnv(t)
				err := op.call(svc, uid, id)
				if !errors.Is(err, op.want[i]) || (op.want[i] == nil && err != nil) {
					t.Fatalf("err = %v, want %v", err, op.want[i])
				}
			})
		}
	}

	t.Run("несуществующее пространство — 404 для всех", func(t *testing.T) {
		svc, _, _, _, _ := newEnv(t)
		if _, err := svc.Get(ctx, owner, 999); !errors.Is(err, split.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGetFillsStatsAndMemberRole(t *testing.T) {
	svc, _, mem, _, id := newEnv(t)
	mem.add(id, 10)
	sp, err := svc.Get(context.Background(), member, id)
	if err != nil {
		t.Fatal(err)
	}
	if sp.MyRole != split.AccessMember || sp.MembersCount != 2 {
		t.Fatalf("%+v", sp)
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()

	t.Run("Draft: правила меняются", func(t *testing.T) {
		svc, _, _, _, id := newEnv(t)
		sp, err := svc.Update(ctx, owner, id, split.Patch{TeamSize: ptr(4), Title: ptr("  Новое  ")})
		if err != nil {
			t.Fatal(err)
		}
		if sp.TeamSize != 4 || sp.Title != "Новое" {
			t.Fatalf("%+v", sp)
		}
	})

	t.Run("Open: смена правил запрещена, те же значения — нет", func(t *testing.T) {
		svc, st, _, _, id := newEnv(t)
		st.setStatus(id, split.StatusOpen)
		if _, err := svc.Update(ctx, owner, id, split.Patch{TeamSize: ptr(4)}); !errors.Is(err, split.ErrRulesLocked) {
			t.Fatalf("err = %v, want ErrRulesLocked", err)
		}
		if _, err := svc.Update(ctx, owner, id, split.Patch{TeamSize: ptr(5), Title: ptr("Ок")}); err != nil {
			t.Fatalf("те же правила должны проходить: %v", err)
		}
	})

	t.Run("Finished: любое изменение запрещено", func(t *testing.T) {
		svc, st, _, _, id := newEnv(t)
		st.setStatus(id, split.StatusFinished)
		if _, err := svc.Update(ctx, owner, id, split.Patch{Title: ptr("Х")}); !errors.Is(err, split.ErrSplitFinished) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("teamSize сливается с текущими ролями", func(t *testing.T) {
		svc, st, _, _, id := newEnv(t)
		_, err := svc.Update(ctx, owner, id, split.Patch{TeamSize: ptr(2)}) // минимумы дают 3
		var ve *split.ValidationError
		if !errors.As(err, &ve) || !ve.Rules {
			t.Fatalf("err = %v", err)
		}
		if st.splits[id].TeamSize != 5 {
			t.Fatal("невалидный патч применился")
		}
	})

	t.Run("maxMembers ниже числа участников", func(t *testing.T) {
		svc, _, _, _, id := newEnv(t) // один участник
		p := split.Patch{MaxMembers: split.Optional[int]{Set: true, Value: ptr(1)}}
		if _, err := svc.Update(ctx, owner, id, p); err != nil {
			t.Fatalf("равно числу участников допустимо: %v", err)
		}
		p.MaxMembers.Value = ptr(0)
		var ve *split.ValidationError
		if _, err := svc.Update(ctx, owner, id, p); !errors.As(err, &ve) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("null снимает необязательные поля", func(t *testing.T) {
		svc, _, _, _, id := newEnv(t)
		when := time.Now().Add(time.Hour)
		if _, err := svc.Update(ctx, owner, id, split.Patch{
			Description:  split.Optional[string]{Set: true, Value: ptr("текст")},
			AutoFinishAt: split.Optional[time.Time]{Set: true, Value: &when},
		}); err != nil {
			t.Fatal(err)
		}
		sp, err := svc.Update(ctx, owner, id, split.Patch{
			Description:  split.Optional[string]{Set: true},
			AutoFinishAt: split.Optional[time.Time]{Set: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if sp.Description != nil || sp.AutoFinishAt != nil {
			t.Fatalf("%+v", sp)
		}
	})
}

func TestChangeStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("допустимая цепочка и событие после коммита", func(t *testing.T) {
		svc, _, _, hub, id := newEnv(t)
		ch, cancel := hub.Subscribe(id)
		defer cancel()

		for _, to := range []split.Status{split.StatusOpen, split.StatusClosed, split.StatusOpen} {
			sp, err := svc.ChangeStatus(ctx, owner, id, to)
			if err != nil || sp.Status != to {
				t.Fatalf("-> %s: %+v err=%v", to, sp, err)
			}
			ev, ok := recv(t, ch)
			if !ok || ev.Type != "status_changed" {
				t.Fatalf("event = %+v", ev)
			}
		}
	})

	t.Run("недопустимый переход без события", func(t *testing.T) {
		svc, _, _, hub, id := newEnv(t)
		ch, cancel := hub.Subscribe(id)
		defer cancel()

		_, err := svc.ChangeStatus(ctx, owner, id, split.StatusFinished)
		var te *split.TransitionError
		if !errors.Is(err, split.ErrInvalidStatusTransition) || !errors.As(err, &te) {
			t.Fatalf("err = %v", err)
		}
		if te.UserMessage() != "Переход из Draft в Finished недопустим" {
			t.Fatalf("message = %q", te.UserMessage())
		}
		if len(ch) != 0 {
			t.Fatal("событие отправлено при неудачном переходе")
		}
	})

	t.Run("ошибка транзакции откатывает и не шлёт событие", func(t *testing.T) {
		svc, fs, _, hub, id := newEnv(t)
		ch, cancel := hub.Subscribe(id)
		defer cancel()
		// failNext срабатывает после fn, то есть как ошибка коммита
		fs.failNext = errBoom
		if _, err := svc.ChangeStatus(ctx, owner, id, split.StatusOpen); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
		if len(ch) != 0 {
			t.Fatal("событие отправлено, хотя коммит не прошёл")
		}
	})
}

func TestDeleteClosesStreams(t *testing.T) {
	svc, st, _, hub, id := newEnv(t)
	ch, cancel := hub.Subscribe(id)
	defer cancel()
	if err := svc.Delete(context.Background(), owner, id); err != nil {
		t.Fatal(err)
	}
	if _, ok := recv(t, ch); ok {
		t.Fatal("поток должен закрыться после удаления")
	}
	if _, exists := st.splits[id]; exists {
		t.Fatal("пространство не удалено")
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	svc, _, mem, _, id := newEnv(t)                 // пространство owner-а, member — участник
	other, err := svc.Create(ctx, member, validNew) // пространство member-а
	if err != nil {
		t.Fatal(err)
	}
	mem.add(other.ID, owner) // owner — участник чужого
	svc.ChangeStatus(ctx, member, other.ID, split.StatusOpen)

	roleOwner, roleMember := "owner", "member"
	open := split.StatusOpen
	tests := []struct {
		name   string
		role   *string
		status *split.Status
		want   map[int64]split.Access
	}{
		{"без фильтров", nil, nil, map[int64]split.Access{id: split.AccessOwner, other.ID: split.AccessMember}},
		{"только свои", &roleOwner, nil, map[int64]split.Access{id: split.AccessOwner}},
		{"только где участник", &roleMember, nil, map[int64]split.Access{other.ID: split.AccessMember}},
		{"по статусу", nil, &open, map[int64]split.Access{other.ID: split.AccessMember}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.List(ctx, owner, tt.role, tt.status)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("получено %d, want %d: %+v", len(got), len(tt.want), got)
			}
			for _, sp := range got {
				if tt.want[sp.ID] != sp.MyRole {
					t.Errorf("split %d: myRole=%s, want %s", sp.ID, sp.MyRole, tt.want[sp.ID])
				}
			}
		})
	}
}
