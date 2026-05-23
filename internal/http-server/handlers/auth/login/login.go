package login

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
	"url-shortener/internal/storage/postgres"
)

type Request struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type Response struct {
	resp.Response
	Token string `json:"token"`
	Email string `json:"email"`
	ID    int64  `json:"id"`
}

type UserGetter interface {
	GetUserByEmail(email string) (*postgres.User, error)
}

func New(log *slog.Logger, userGetter UserGetter, jwtSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.auth.login.New"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req Request
		if err := render.DecodeJSON(r.Body, &req); err != nil {
			log.Error("failed to decode request body", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("failed to decode request"))
			return
		}

		if err := validator.New().Struct(req); err != nil {
			validateErr := err.(validator.ValidationErrors)
			log.Error("invalid request", sl.Err(err))
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.ValidationError(validateErr))
			return
		}

		user, err := userGetter.GetUserByEmail(req.Email)
		if errors.Is(err, storage.ErrUserNotFound) {
			log.Info("user not found", slog.String("email", req.Email))
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, resp.Error("invalid credentials"))
			return
		}
		if err != nil {
			log.Error("failed to get user", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			log.Info("invalid password", slog.String("email", req.Email))
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, resp.Error("invalid credentials"))
			return
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": user.ID,
			"email":   user.Email,
			"exp":     time.Now().Add(72 * time.Hour).Unix(),
		})

		tokenStr, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			log.Error("failed to sign token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to generate token"))
			return
		}

		log.Info("user logged in", slog.String("email", req.Email))

		render.JSON(w, r, Response{
			Response: resp.OK(),
			Token:    tokenStr,
			Email:    user.Email,
			ID:       user.ID,
		})
	}
}
