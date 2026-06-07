package logout

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"

	resp "url-shortener/internal/lib/api/response"
)

type TokenBlacklist struct {
	mu     sync.RWMutex
	tokens map[string]time.Time // token -> expiry
}

var Blacklist = &TokenBlacklist{
	tokens: make(map[string]time.Time),
}

func (b *TokenBlacklist) Add(token string, expiry time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens[token] = expiry
	now := time.Now()
	for t, exp := range b.tokens {
		if now.After(exp) {
			delete(b.tokens, t)
		}
	}
}

func (b *TokenBlacklist) IsBlacklisted(token string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	exp, ok := b.tokens[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		return false
	}
	return true
}

func New(log *slog.Logger, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.auth.logout.New"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("missing authorization header"))
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid authorization header format"))
			return
		}

		tokenStr := parts[1]

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})
		if err != nil || !token.Valid {
			log.Info("logout with invalid token (already expired or invalid)")
			render.JSON(w, r, resp.OK())
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid token claims"))
			return
		}

		expFloat, ok := claims["exp"].(float64)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid exp in token"))
			return
		}
		expiry := time.Unix(int64(expFloat), 0)

		Blacklist.Add(tokenStr, expiry)

		log.Info("user logged out", slog.String("token_prefix", tokenStr[:min(10, len(tokenStr))]))

		render.JSON(w, r, resp.OK())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
