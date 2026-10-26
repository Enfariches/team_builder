package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"builder/backend/internal/platform/pgtest"
	"builder/backend/internal/user"
)

func TestRepositoryOnPostgres(t *testing.T) {
	ctx := context.Background()
	repo := user.NewRepository(pgtest.New(t))

	a, err := repo.Create(ctx, user.NewAccount{
		Login: "ivan", PasswordHash: "h", FirstName: "Иван",
		MiddleName: ptr("Иванович"), LastName: "Иванов", Link: ptr("@ivan"),
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Create(ctx, user.NewAccount{Login: "petr", PasswordHash: "h", FirstName: "Пётр", LastName: "Петров"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, user.NewAccount{Login: "ivan", PasswordHash: "h", FirstName: "Х", LastName: "Х"}); !errors.Is(err, user.ErrLoginTaken) {
		t.Fatalf("дубль логина: %v", err)
	}

	t.Run("частичное обновление меняет только переданное", func(t *testing.T) {
		time.Sleep(10 * time.Millisecond)
		got, err := repo.Update(ctx, a.ID, user.ProfileUpdate{FirstName: ptr("Игорь")})
		if err != nil {
			t.Fatal(err)
		}
		if got.FirstName != "Игорь" || got.LastName != "Иванов" ||
			got.MiddleName == nil || *got.MiddleName != "Иванович" || got.Link == nil || *got.Link != "@ivan" {
			t.Fatalf("затронуты лишние поля: %+v", got.User)
		}
	})

	t.Run("null очищает, значение перезаписывает", func(t *testing.T) {
		got, err := repo.Update(ctx, a.ID, user.ProfileUpdate{
			MiddleName: user.Optional[string]{Set: true},
			Link:       user.Optional[string]{Set: true, Value: ptr("@new")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.MiddleName != nil || got.Link == nil || *got.Link != "@new" {
			t.Fatalf("middleName=%v link=%v", got.MiddleName, got.Link)
		}
		if got.FirstName != "Игорь" {
			t.Fatalf("firstName затронут: %q", got.FirstName)
		}
	})

	t.Run("несуществующий id", func(t *testing.T) {
		if _, err := repo.Update(ctx, 9999, user.ProfileUpdate{FirstName: ptr("Х")}); !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})

	t.Run("ListByIDs пропускает отсутствующих", func(t *testing.T) {
		got, err := repo.ListByIDs(ctx, []int64{b.ID, a.ID, 9999})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
			t.Fatalf("got %+v", got)
		}
	})
}
