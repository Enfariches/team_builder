package tests

import (
	"builder/backend/internal/auth"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenIssuerParse(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	issuer := auth.NewTokenIssuer(secret, 15*time.Minute)
	issuer.SetClock(func() time.Time { return now })

	valid, err := issuer.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	other := auth.NewTokenIssuer([]byte("ffffffffffffffffffffffffffffffff"), time.Minute)
	other.SetClock(func() time.Time { return now })
	foreign, _ := other.Issue(42)

	noneToken, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
		Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.RegisteredClaims{
		Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}).SignedString(secret)
	noExp, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "42"}).SignedString(secret)

	tests := []struct {
		name    string
		token   string
		now     time.Time
		wantID  int64
		wantErr bool
	}{
		{"валидный", valid, now, 42, false},
		{"просрочен", valid, now.Add(16 * time.Minute), 0, true},
		{"чужая подпись", foreign, now, 0, true},
		{"alg=none", noneToken, now, 0, true},
		{"другой HMAC-алгоритм", hs512, now, 0, true},
		{"без exp", noExp, now, 0, true},
		{"мусор", "abc.def.ghi", now, 0, true},
		{"пусто", "", now, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issuer.SetClock(func() time.Time { return tt.now })
			id, err := issuer.Parse(tt.token)
			if (err != nil) != tt.wantErr || id != tt.wantID {
				t.Fatalf("Parse = (%d, %v), want (%d, err=%v)", id, err, tt.wantID, tt.wantErr)
			}
		})
	}
}

func TestRefreshTokenHash(t *testing.T) {
	tok, hash, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if string(auth.HashRefreshToken(tok)) != string(hash) {
		t.Fatal("хеш не совпадает с хешем токена")
	}
	tok2, _, _ := auth.NewRefreshToken()
	if tok == tok2 {
		t.Fatal("токены повторяются")
	}
}
