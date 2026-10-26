package tests

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/platform/pgtest"
	"builder/backend/internal/split"
)

func newPgEnv(t *testing.T) (*split.Service, *pgxpool.Pool, *fakeMembers) {
	t.Helper()
	pool := pgtest.New(t)
	for _, login := range []string{"owner", "member", "stranger"} {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO users (login, password_hash, first_name, last_name) VALUES ($1, 'h', 'Имя', 'Фамилия')`, login); err != nil {
			t.Fatal(err)
		}
	}
	mem := newFakeMembers()
	svc := split.NewService(split.NewRepository(pool), split.NewPgTxRunner(pool), mem, mem, split.NewHub())
	return svc, pool, mem
}

func TestSplitsOnPostgres(t *testing.T) {
	ctx := context.Background()
	svc, pool, mem := newPgEnv(t) // owner=1, member=2, stranger=3

	sp, err := svc.Create(ctx, owner, split.NewSplit{
		Title: "Практикум", Description: ptr("описание"), TeamSize: 5, MaxMembers: ptr(30),
		RemainderPolicy: split.PolicyWaitlist,
		Roles: []split.RoleInput{
			{Name: "Backend", PerTeamMin: 1, PerTeamMax: 2},
			{Name: "Designer", PerTeamMin: 1, PerTeamMax: 1},
			{Name: "Frontend", PerTeamMin: 1, PerTeamMax: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("чтение возвращает записанное, роли в заданном порядке", func(t *testing.T) {
		got, err := svc.Get(ctx, owner, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Практикум" || *got.Description != "описание" || *got.MaxMembers != 30 ||
			got.RemainderPolicy != split.PolicyWaitlist || got.Status != split.StatusDraft {
			t.Fatalf("%+v", got)
		}
		names := []string{}
		for _, r := range got.Roles {
			names = append(names, r.Name)
		}
		if len(names) != 3 || names[0] != "Backend" || names[1] != "Designer" || names[2] != "Frontend" {
			t.Fatalf("порядок ролей: %v", names)
		}
	})

	t.Run("частичное обновление и замена ролей", func(t *testing.T) {
		newRoles := []split.RoleInput{{Name: "Fullstack", PerTeamMin: 1, PerTeamMax: 6}}
		got, err := svc.Update(ctx, owner, sp.ID, split.Patch{
			Title: ptr("Новое"), Roles: &newRoles,
			MaxMembers: split.Optional[int]{Set: true}, // null снимает ограничение
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "Новое" || got.MaxMembers != nil || got.Description == nil || len(got.Roles) != 1 || got.Roles[0].Name != "Fullstack" {
			t.Fatalf("%+v", got)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM split_roles WHERE split_id = $1`, sp.ID).Scan(&n); err != nil || n != 1 {
			t.Fatalf("ролей в БД = %d, err=%v", n, err)
		}
	})

	t.Run("список с фильтрами", func(t *testing.T) {
		other, err := svc.Create(ctx, member, validNew)
		if err != nil {
			t.Fatal(err)
		}
		mem.add(other.ID, owner)
		if _, err := svc.ChangeStatus(ctx, member, other.ID, split.StatusOpen); err != nil {
			t.Fatal(err)
		}
		roleMember, open := "member", split.StatusOpen

		all, _ := svc.List(ctx, owner, nil, nil)
		asMember, _ := svc.List(ctx, owner, &roleMember, nil)
		byStatus, _ := svc.List(ctx, owner, nil, &open)
		if len(all) != 2 || len(asMember) != 1 || asMember[0].ID != other.ID || len(byStatus) != 1 || byStatus[0].ID != other.ID {
			t.Fatalf("all=%d member=%d open=%d", len(all), len(asMember), len(byStatus))
		}
		if all[0].ID != other.ID {
			t.Fatal("новые пространства должны идти первыми")
		}
		if none, _ := svc.List(ctx, stranger, nil, nil); len(none) != 0 {
			t.Fatalf("у постороннего %d пространств", len(none))
		}
	})

	t.Run("удаление каскадно чистит роли", func(t *testing.T) {
		if err := svc.Delete(ctx, owner, sp.ID); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM split_roles WHERE split_id = $1`, sp.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("ролей после удаления = %d, err=%v", n, err)
		}
		if _, err := svc.Get(ctx, owner, sp.ID); !errors.Is(err, split.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

// Параллельные смены статуса сериализуются через SELECT ... FOR UPDATE:
// из N одинаковых переходов Draft -> Open успешен ровно один.
func TestConcurrentStatusChange(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newPgEnv(t)
	sp, err := svc.Create(ctx, owner, validNew)
	if err != nil {
		t.Fatal(err)
	}

	const n = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		ok, conf int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ChangeStatus(ctx, owner, sp.ID, split.StatusOpen)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ok++
			case errors.Is(err, split.ErrInvalidStatusTransition):
				conf++
			default:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok != 1 || conf != n-1 {
		t.Fatalf("успешных=%d, конфликтов=%d, want 1 и %d", ok, conf, n-1)
	}
}
