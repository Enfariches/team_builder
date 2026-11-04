package tests

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"builder/backend/internal/invite"
)

func ptr[T any](v T) *T { return &v }

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func TestValidAt(t *testing.T) {
	tests := []struct {
		name string
		inv  invite.Invite
		want bool
	}{
		{"бессрочный и без лимита", invite.Invite{}, true},
		{"отозван", invite.Invite{RevokedAt: ptr(t0.Add(-time.Hour))}, false},
		{"не истёк", invite.Invite{ExpiresAt: ptr(t0.Add(time.Second))}, true},
		{"истёк ровно сейчас", invite.Invite{ExpiresAt: ptr(t0)}, false},
		{"истёк давно", invite.Invite{ExpiresAt: ptr(t0.Add(-time.Hour))}, false},
		{"лимит не исчерпан", invite.Invite{MaxUses: ptr(3), UsesCount: 2}, true},
		{"лимит исчерпан", invite.Invite{MaxUses: ptr(3), UsesCount: 3}, false},
		{"отозван и истёк", invite.Invite{RevokedAt: ptr(t0), ExpiresAt: ptr(t0)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.inv.ValidAt(t0); got != tt.want {
				t.Fatalf("ValidAt = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewInviteValidate(t *testing.T) {
	tests := []struct {
		name string
		in   invite.NewInvite
		want []string
	}{
		{"пустой", invite.NewInvite{}, nil},
		{"будущее", invite.NewInvite{ExpiresAt: ptr(t0.Add(time.Minute))}, nil},
		{"сейчас", invite.NewInvite{ExpiresAt: ptr(t0)}, []string{"expiresAt"}},
		{"прошлое", invite.NewInvite{ExpiresAt: ptr(t0.Add(-time.Minute))}, []string{"expiresAt"}},
		{"maxUses 1", invite.NewInvite{MaxUses: ptr(1)}, nil},
		{"maxUses 0", invite.NewInvite{MaxUses: ptr(0)}, []string{"maxUses"}},
		{"обе проблемы", invite.NewInvite{ExpiresAt: ptr(t0), MaxUses: ptr(-1)}, []string{"expiresAt", "maxUses"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate(t0)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("неожиданная ошибка: %v", err)
				}
				return
			}
			var ve *invite.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			var got []string
			for _, p := range ve.Problems {
				got = append(got, p.Field)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("поля = %v, want %v", got, tt.want)
			}
		})
	}
}
