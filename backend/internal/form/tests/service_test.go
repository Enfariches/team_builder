package tests

import (
	"context"
	"errors"
	"testing"

	"builder/backend/internal/form"
)

// fakeStore — рукописный двойник репозитория; анкеты хранятся по id,
// владелец проверяется так же, как в SQL.
type fakeStore struct {
	forms       map[int64]form.Form
	nextID      int64
	createCalls int
	updateCalls int
	lastUpdate  form.Update
}

func newFakeStore() *fakeStore {
	return &fakeStore{forms: map[int64]form.Form{
		1: {ID: 1, UserID: 1, Name: "Моя", Experience: 3, Skills: []string{"Go"}, RolePreferences: []form.RolePreference{{RoleName: "Backend", Rank: 1}}},
		2: {ID: 2, UserID: 2, Name: "Чужая"},
	}, nextID: 10}
}

func (f *fakeStore) Create(_ context.Context, uid int64, n form.NewForm) (form.Form, error) {
	f.createCalls++
	f.nextID++
	out := form.Form{ID: f.nextID, UserID: uid, Name: n.Name, Experience: n.Experience, Skills: n.Skills, RolePreferences: n.RolePreferences, Comment: n.Comment}
	f.forms[out.ID] = out
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, uid, id int64) (form.Form, error) {
	x, ok := f.forms[id]
	if !ok || x.UserID != uid {
		return form.Form{}, form.ErrNotFound
	}
	return x, nil
}

func (f *fakeStore) List(_ context.Context, uid int64) ([]form.Form, error) {
	var out []form.Form
	for _, x := range f.forms {
		if x.UserID == uid {
			out = append(out, x)
		}
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, uid, id int64, u form.Update) (form.Form, error) {
	f.updateCalls++
	f.lastUpdate = u
	x, err := f.Get(context.Background(), uid, id)
	if err != nil {
		return form.Form{}, err
	}
	if u.Name != nil {
		x.Name = *u.Name
	}
	f.forms[id] = x
	return x, nil
}

func (f *fakeStore) Delete(_ context.Context, uid, id int64) error {
	if _, err := f.Get(context.Background(), uid, id); err != nil {
		return err
	}
	delete(f.forms, id)
	return nil
}

func TestCreateValidationDoesNotReachStore(t *testing.T) {
	st := newFakeStore()
	_, err := form.NewService(st).Create(context.Background(), 1, form.NewForm{Name: "", Experience: 99})
	var ve *form.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if st.createCalls != 0 {
		t.Fatal("невалидная анкета дошла до store")
	}
}

func TestUpdateEmptyPatchDoesNotWrite(t *testing.T) {
	st := newFakeStore()
	f, err := form.NewService(st).Update(context.Background(), 1, 1, form.Update{})
	if err != nil {
		t.Fatal(err)
	}
	if st.updateCalls != 0 || f.Name != "Моя" {
		t.Fatalf("updateCalls=%d form=%+v", st.updateCalls, f)
	}
}

func TestUpdateNormalizesBeforeStore(t *testing.T) {
	st := newFakeStore()
	if _, err := form.NewService(st).Update(context.Background(), 1, 1, form.Update{Name: ptr("  Новая  ")}); err != nil {
		t.Fatal(err)
	}
	if st.lastUpdate.Name == nil || *st.lastUpdate.Name != "Новая" {
		t.Fatalf("name = %v", st.lastUpdate.Name)
	}
}

func TestForeignFormIsNotFound(t *testing.T) {
	svc := form.NewService(newFakeStore())
	ctx := context.Background()
	checks := map[string]func() error{
		"Get":      func() error { _, err := svc.Get(ctx, 1, 2); return err },
		"Update":   func() error { _, err := svc.Update(ctx, 1, 2, form.Update{Name: ptr("x")}); return err },
		"Empty":    func() error { _, err := svc.Update(ctx, 1, 2, form.Update{}); return err },
		"Delete":   func() error { return svc.Delete(ctx, 1, 2) },
		"Snapshot": func() error { _, err := svc.Snapshot(ctx, 1, 2); return err },
	}
	for name, fn := range checks {
		if err := fn(); !errors.Is(err, form.ErrNotFound) {
			t.Errorf("%s чужой анкеты: %v, want ErrNotFound", name, err)
		}
	}
}

func TestSnapshot(t *testing.T) {
	s, err := form.NewService(newFakeStore()).Snapshot(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if s.Experience != 3 || len(s.Skills) != 1 || s.RolePreferences[0].RoleName != "Backend" {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestListOnlyOwn(t *testing.T) {
	fs, err := form.NewService(newFakeStore()).List(context.Background(), 1)
	if err != nil || len(fs) != 1 || fs[0].ID != 1 {
		t.Fatalf("fs=%+v err=%v", fs, err)
	}
}
