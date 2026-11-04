package invite

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// newToken: 16 байт из crypto/rand, base64url без паддинга (22 символа).
// Токен хранится открыто: владелец видит его в списке, а предпросмотру нужен
// поиск по нему. Угадать его перебором нельзя.
func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("invite.newToken: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
