package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"builder/backend/internal/invite"
)

const (
	owner    int64 = 1
	member   int64 = 2
	stranger int64 = 3
	splitID  int64 = 100
)

// fakeStore — рукописный двойник репозитория.
type fakeStore struct {
	invites map[int64]invite.Invite
	nextID  int64
}

func newFakeStore() *fakeStore { return &fakeStore{invites: map[int64]invite.Invite{}} }

func (f *fakeStore) Create(_ context.Context, splitID int64, token string, n invite.NewInvite) (invite.Invite, error) {
	f.nextID++
	inv := invite.Invite{ID: f.nextID, SplitID: splitID, Token: token, ExpiresAt: n.ExpiresAt, MaxUses: n.MaxUses, CreatedAt: t0}
	f.invites[inv.ID] = inv
	return inv, nil
}

func (f *fakeStore) List(_ context.Context, splitID int64) ([]invite.Invite, error) {
	var out []invite.Invite
	for _, inv := range f.invites {
		if inv.SplitID == splitID {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (f *fakeStore) GetByToken(_ context.Context, token string) (invite.Invite, error) {
	for _, inv := range f.invites {
		if inv.Token == token {
			return inv, nil
		}
	}
	return invite.Invite{}, invite.ErrNotFound
}

func (f *fakeStore) Revoke(_ context.Context, splitID, id int64, now time.Time) error {
	inv, ok := f.invites[id]
	if !ok || inv.SplitID != splitID {
		return invite.ErrNotFound
	}
	if inv.RevokedAt == nil {
		inv.RevokedAt = &now
		f.invites[id] = inv
	}
	return nil
}

func (f *fakeStore) Redeem(context.Context, string, time.Time) (invite.Redemption, error) {
	return invite.Redemption{}, errors.New("not used")
}

func (f *fakeStore) Release(context.Context, int64) error { return errors.New("not used") }

// fakeSplits реализует оба порта: owner владеет пространством splitID, member — участник.
type fakeSplits struct{ infoCalls int }

func (f *fakeSplits) Authorize(_ context.Context, userID, id int64) (invite.Access, error) {
	switch {
	case id != splitID:
		return "", invite.ErrSplitNotFound
	case userID == owner:
		return invite.AccessOwner, nil
	case userID == member:
		return invite.AccessMember, nil
	}
	return "", invite.ErrSplitNotFound
}

func (f *fakeSplits) SplitInfo(_ context.Context, id int64) (invite.SplitInfo, error) {
	f.infoCalls++
	if id != splitID {
		return invite.SplitInfo{}, invite.ErrSplitNotFound
	}
	return invite.SplitInfo{Title: "Практикум", TeamSize: 5, RoleNames: []string{"Backend"}, MembersCount: 2, Status: "Open"}, nil
}

func newService() (*invite.Service, *fakeStore, *fakeSplits) {
	st, sp := newFakeStore(), &fakeSplits{}
	svc := invite.NewService(st, sp, sp)
	svc.SetClock(func() time.Time { return t0 })
	n := 0
	svc.SetTokenGenerator(func() (string, error) { n++; return "tok" + string(rune('0'+n)), nil })
	return svc, st, sp
}

func TestAccessMatrix(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newService()
	inv, err := svc.Create(ctx, owner, splitID, invite.NewInvite{})
	if err != nil {
		t.Fatal(err)
	}

	ops := map[string]func(user, split int64) error{
		"List":   func(u, s int64) error { _, err := svc.List(ctx, u, s); return err },
		"Create": func(u, s int64) error { _, err := svc.Create(ctx, u, s, invite.NewInvite{}); return err },
		"Revoke": func(u, s int64) error { return svc.Revoke(ctx, u, s, inv.ID) },
	}
	users := []struct {
		name  string
		user  int64
		split int64
		want  error
	}{
		{"владелец", owner, splitID, nil},
		{"участник", member, splitID, invite.ErrForbidden},
		{"посторонний", stranger, splitID, invite.ErrSplitNotFound},
		{"нет пространства", owner, 999, invite.ErrSplitNotFound},
	}
	for name, op := range ops {
		for _, u := range users {
			t.Run(name+"/"+u.name, func(t *testing.T) {
				err := op(u.user, u.split)
				if u.want == nil && err != nil || u.want != nil && !errors.Is(err, u.want) {
					t.Fatalf("err = %v, want %v", err, u.want)
				}
			})
		}
	}
	_ = st
}

func TestCreateRejectsPastExpiry(t *testing.T) {
	svc, st, _ := newService()
	_, err := svc.Create(context.Background(), owner, splitID, invite.NewInvite{ExpiresAt: ptr(t0.Add(-time.Minute))})
	var ve *invite.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
	if len(st.invites) != 0 {
		t.Fatal("невалидный инвайт сохранён")
	}
}

func TestCreateGeneratesToken(t *testing.T) {
	svc, _, _ := newService()
	inv, err := svc.Create(context.Background(), owner, splitID, invite.NewInvite{})
	if err != nil || inv.Token != "tok1" || inv.SplitID != splitID {
		t.Fatalf("inv=%+v err=%v", inv, err)
	}
}

func TestRevokeIsIdempotentAndScoped(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newService()
	inv, _ := svc.Create(ctx, owner, splitID, invite.NewInvite{})

	if err := svc.Revoke(ctx, owner, splitID, inv.ID); err != nil {
		t.Fatal(err)
	}
	first := *st.invites[inv.ID].RevokedAt
	svc.SetClock(func() time.Time { return t0.Add(time.Hour) })
	if err := svc.Revoke(ctx, owner, splitID, inv.ID); err != nil {
		t.Fatalf("повторный отзыв: %v", err)
	}
	if !st.invites[inv.ID].RevokedAt.Equal(first) {
		t.Fatal("повторный отзыв перезаписал revoked_at")
	}
	if err := svc.Revoke(ctx, owner, splitID, 12345); !errors.Is(err, invite.ErrNotFound) {
		t.Fatalf("несуществующий инвайт: %v", err)
	}
}

func TestPreview(t *testing.T) {
	ctx := context.Background()

	t.Run("действительный", func(t *testing.T) {
		svc, _, _ := newService()
		inv, _ := svc.Create(ctx, owner, splitID, invite.NewInvite{})
		info, err := svc.Preview(ctx, inv.Token)
		if err != nil || info.Title != "Практикум" || info.MembersCount != 2 {
			t.Fatalf("info=%+v err=%v", info, err)
		}
	})

	t.Run("недействительные не доходят до пространства", func(t *testing.T) {
		svc, st, sp := newService()
		revoked, _ := svc.Create(ctx, owner, splitID, invite.NewInvite{})
		_ = svc.Revoke(ctx, owner, splitID, revoked.ID)
		expired, _ := svc.Create(ctx, owner, splitID, invite.NewInvite{ExpiresAt: ptr(t0.Add(time.Minute))})
		exhausted, _ := svc.Create(ctx, owner, splitID, invite.NewInvite{MaxUses: ptr(1)})
		e := st.invites[exhausted.ID]
		e.UsesCount = 1
		st.invites[exhausted.ID] = e
		svc.SetClock(func() time.Time { return t0.Add(time.Hour) })

		for name, token := range map[string]string{
			"отозван": revoked.Token, "истёк": expired.Token, "исчерпан": exhausted.Token, "неизвестный": "нет-такого",
		} {
			if _, err := svc.Preview(ctx, token); !errors.Is(err, invite.ErrInviteInvalid) {
				t.Errorf("%s: err = %v, want ErrInviteInvalid", name, err)
			}
		}
		if sp.infoCalls != 0 {
			t.Fatalf("SplitInfo вызван %d раз для недействительных инвайтов", sp.infoCalls)
		}
	})
}
