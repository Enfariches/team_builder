package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const refreshTokenBytes = 32

// TokenIssuer выпускает и проверяет access-JWT (HS256).
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenIssuer(secret []byte, ttl time.Duration) *TokenIssuer {
	return &TokenIssuer{secret: secret, ttl: ttl, now: time.Now}
}

// SetClock подменяет источник времени; нужен тестам истечения токенов.
func (t *TokenIssuer) SetClock(now func() time.Time) { t.now = now }

func (t *TokenIssuer) TTL() time.Duration { return t.ttl }

func (t *TokenIssuer) Issue(userID int64) (string, error) {
	now := t.now()
	jti, err := randomString(12)
	if err != nil {
		return "", err
	}
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(userID, 10),
		ID:        jti,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// Parse возвращает id пользователя; алгоритм зафиксирован явно, чтобы
// исключить подмену на alg=none.
func (t *TokenIssuer) Parse(token string) (int64, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return 0, fmt.Errorf("auth.TokenIssuer.Parse: %v: %w", err, ErrUnauthorized)
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("auth.TokenIssuer.Parse: bad subject %q: %w", claims.Subject, ErrUnauthorized)
	}
	return id, nil
}

// NewRefreshToken возвращает непрозрачный токен для клиента и его хеш для БД.
func NewRefreshToken() (token string, hash []byte, err error) {
	token, err = randomString(refreshTokenBytes)
	if err != nil {
		return "", nil, err
	}
	return token, HashRefreshToken(token), nil
}

// SHA-256 без соли достаточно: токен — 256 бит энтропии, перебор невозможен.
func HashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
