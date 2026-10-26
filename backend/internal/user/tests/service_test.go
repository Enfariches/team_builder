package tests

import (
	"context"
	"errors"
	"testing"

	"builder/backend/internal/user"
)

// fakeStore — рукописный двойник репозитория.
type fakeStore struct {
	users       map[int64]user.Account
	updateCalls int
	lastUpdate  user.ProfileUpdate
	listCalls   int
	listedIDs   []int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[int64]user.Account{
		1: {User: user.User{ID: 1, Login: "ivan", FirstName: "Иван", LastName: "Иванов"}},
		2: {User: user.User{ID: 2, Login: "petr", FirstName: "Пётр", LastName: "Петров"}},
	}}
}

func (f *fakeStore) Create(context.Context, user.NewAccount) (user.Account, error) {
	return user.Account{}, errors.New("not used")
}

func (f *fakeStore) GetByLogin(context.Context, string) (user.Account, error) {
	return user.Account{}, errors.New("not used")
}

func (f *fakeStore) GetByID(_ context.Context, id int64) (user.Account, error) {
	a, ok := f.users[id]
	if !ok {
		return user.Account{}, user.ErrNotFound
	}
	return a, nil
}

func (f *fakeStore) Update(_ context.Context, id int64, u user.ProfileUpdate) (user.Account, error) {
	f.updateCalls++
	f.lastUpdate = u
	a, ok := f.users[id]
	if !ok {
		return user.Account{}, user.ErrNotFound
	}
	if u.FirstName != nil {
		a.FirstName = *u.FirstName
	}
	f.users[id] = a
	return a, nil
}

func (f *fakeStore) ListByIDs(_ context.Context, ids []int64) ([]user.User, error) {
	f.listCalls++
	f.listedIDs = ids
	var out []user.User
	for _, id := range ids {
		if a, ok := f.users[id]; ok {
			out = append(out, a.User)
		}
	}
	return out, nil
}

func TestUpdateProfile(t *testing.T) {
	ctx := context.Background()

	t.Run("применяет и нормализует", func(t *testing.T) {
		st := newFakeStore()
		got, err := user.NewService(st).UpdateProfile(ctx, 1, user.ProfileUpdate{FirstName: ptr("  Игорь ")})
		if err != nil {
			t.Fatal(err)
		}
		if got.FirstName != "Игорь" || st.updateCalls != 1 {
			t.Fatalf("got %+v, calls=%d", got, st.updateCalls)
		}
	})

	t.Run("пустой патч не пишет в БД", func(t *testing.T) {
		st := newFakeStore()
		got, err := user.NewService(st).UpdateProfile(ctx, 1, user.ProfileUpdate{})
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != 1 || st.updateCalls != 0 {
			t.Fatalf("got %+v, update calls=%d", got, st.updateCalls)
		}
	})

	t.Run("ошибка валидации не доходит до БД", func(t *testing.T) {
		st := newFakeStore()
		_, err := user.NewService(st).UpdateProfile(ctx, 1, user.ProfileUpdate{FirstName: ptr("  ")})
		var ve *user.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("err = %v, want ValidationError", err)
		}
		if st.updateCalls != 0 {
			t.Fatal("Update вызван при невалидных данных")
		}
	})

	t.Run("несуществующий пользователь", func(t *testing.T) {
		for _, u := range []user.ProfileUpdate{{FirstName: ptr("Х")}, {}} {
			_, err := user.NewService(newFakeStore()).UpdateProfile(ctx, 99, u)
			if !errors.Is(err, user.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		}
	})
}

func TestListBriefs(t *testing.T) {
	ctx := context.Background()

	t.Run("дубли схлопываются, отсутствующие пропускаются", func(t *testing.T) {
		st := newFakeStore()
		got, err := user.NewService(st).ListBriefs(ctx, []int64{2, 1, 2, 99})
		if err != nil {
			t.Fatal(err)
		}
		if len(st.listedIDs) != 3 || len(got) != 2 {
			t.Fatalf("запрошено %v, получено %d", st.listedIDs, len(got))
		}
	})

	t.Run("пустой список без запроса", func(t *testing.T) {
		st := newFakeStore()
		got, err := user.NewService(st).ListBriefs(ctx, nil)
		if err != nil || got != nil || st.listCalls != 0 {
			t.Fatalf("got=%v err=%v calls=%d", got, err, st.listCalls)
		}
	})
}
