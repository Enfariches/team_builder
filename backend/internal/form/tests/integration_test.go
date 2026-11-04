package tests

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"builder/backend/internal/form"
	"builder/backend/internal/platform/pgtest"
	"builder/backend/internal/user"
)

func newUser(t *testing.T, pool *pgxpool.Pool, login string) int64 {
	t.Helper()
	acc, err := user.NewRepository(pool).Create(context.Background(), user.NewAccount{
		Login: login, PasswordHash: "h", FirstName: "Имя", LastName: "Фамилия",
	})
	if err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

func TestRepositoryOnPostgres(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)
	repo := form.NewRepository(pool)
	alice, bob := newUser(t, pool, "alice"), newUser(t, pool, "bob")

	created, err := repo.Create(ctx, alice, form.NewForm{
		Name: "Основная", Experience: 3, Skills: []string{"Go", "PostgreSQL"},
		RolePreferences: []form.RolePreference{{RoleName: "Backend", Rank: 1}, {RoleName: "QA", Rank: 2}}, Comment: ptr("привет"),
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("jsonb и text[] читаются обратно", func(t *testing.T) {
		got, err := repo.Get(ctx, alice, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Skills, []string{"Go", "PostgreSQL"}) ||
			!reflect.DeepEqual(got.RolePreferences, []form.RolePreference{{RoleName: "Backend", Rank: 1}, {RoleName: "QA", Rank: 2}}) ||
			got.Comment == nil || *got.Comment != "привет" || got.Experience != 3 {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("пустые навыки и предпочтения — пустые срезы, не nil", func(t *testing.T) {
		f, err := repo.Create(ctx, alice, form.NewForm{Name: "Пустая"})
		if err != nil {
			t.Fatal(err)
		}
		if f.Skills == nil || f.RolePreferences == nil || f.Comment != nil {
			t.Fatalf("f = %+v", f)
		}
	})

	t.Run("у пользователя несколько анкет", func(t *testing.T) {
		list, err := repo.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].ID != created.ID {
			t.Fatalf("list = %+v", list)
		}
	})

	t.Run("изоляция пользователей", func(t *testing.T) {
		if list, _ := repo.List(ctx, bob); len(list) != 0 {
			t.Fatalf("bob видит чужие анкеты: %+v", list)
		}
		if _, err := repo.Get(ctx, bob, created.ID); !errors.Is(err, form.ErrNotFound) {
			t.Fatalf("Get чужой: %v", err)
		}
		if _, err := repo.Update(ctx, bob, created.ID, form.Update{Name: ptr("взлом")}); !errors.Is(err, form.ErrNotFound) {
			t.Fatalf("Update чужой: %v", err)
		}
		if err := repo.Delete(ctx, bob, created.ID); !errors.Is(err, form.ErrNotFound) {
			t.Fatalf("Delete чужой: %v", err)
		}
		if got, _ := repo.Get(ctx, alice, created.ID); got.Name != "Основная" {
			t.Fatalf("чужая правка прошла: %+v", got)
		}
	})

	t.Run("частичное обновление меняет только переданное", func(t *testing.T) {
		time.Sleep(10 * time.Millisecond)
		got, err := repo.Update(ctx, alice, created.ID, form.Update{Name: ptr("Новая")})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "Новая" || got.Experience != 3 || len(got.Skills) != 2 ||
			len(got.RolePreferences) != 2 || got.Comment == nil || !got.UpdatedAt.After(got.CreatedAt) {
			t.Fatalf("затронуты лишние поля: %+v", got)
		}
	})

	t.Run("пустой срез очищает, null очищает comment", func(t *testing.T) {
		got, err := repo.Update(ctx, alice, created.ID, form.Update{
			Skills:          &[]string{},
			RolePreferences: &[]form.RolePreference{},
			Comment:         form.Optional[string]{Set: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Skills) != 0 || len(got.RolePreferences) != 0 || got.Comment != nil || got.Name != "Новая" {
			t.Fatalf("got = %+v", got)
		}
	})

	t.Run("удаление и каскад от пользователя", func(t *testing.T) {
		if err := repo.Delete(ctx, alice, created.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx, alice, created.ID); !errors.Is(err, form.ErrNotFound) {
			t.Fatalf("после удаления: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, alice); err != nil {
			t.Fatal(err)
		}
		if list, _ := repo.List(ctx, alice); len(list) != 0 {
			t.Fatalf("анкеты удалённого пользователя остались: %+v", list)
		}
	})
}
