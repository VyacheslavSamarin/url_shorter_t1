package verify

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"

	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
	"url-shortener/internal/storage/postgres"
)

type ConfirmRequest struct {
	Email string `json:"email" validate:"required,email"`
	Code  string `json:"code" validate:"required"`
}

type ConfirmResponse struct {
	resp.Response
	Token  string `json:"token"`
	Email  string `json:"email"`
	UserID int64  `json:"user_id"`
}

type VerificationStore interface {
	GetEmailVerification(email, code string) (*postgres.EmailVerification, error)
	GetEmailVerificationByEmail(email string) (*postgres.EmailVerification, error)
	DeleteEmailVerification(email string) error
	CreateUser(email, passwordHash string) (int64, error)
}

func NewConfirmCode(log *slog.Logger, store VerificationStore, jwtSecret string, skipVerify bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.auth.verify.NewConfirmCode"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req ConfirmRequest
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

		var verification *postgres.EmailVerification

		if skipVerify {
			v, err := store.GetEmailVerificationByEmail(req.Email)
			if errors.Is(err, storage.ErrVerificationNotFound) {
				log.Info("verification not found (skip mode)", slog.String("email", req.Email))
				w.WriteHeader(http.StatusBadRequest)
				render.JSON(w, r, resp.Error("send code first"))
				return
			}
			if err != nil {
				log.Error("failed to get verification", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, resp.Error("internal error"))
				return
			}
			verification = v
			log.Info("email verification skipped", slog.String("email", req.Email))
		} else {
			v, err := store.GetEmailVerification(req.Email, req.Code)
			if errors.Is(err, storage.ErrVerificationNotFound) {
				log.Info("verification not found", slog.String("email", req.Email))
				w.WriteHeader(http.StatusBadRequest)
				render.JSON(w, r, resp.Error("invalid or expired code"))
				return
			}
			if err != nil {
				log.Error("failed to get verification", sl.Err(err))
				w.WriteHeader(http.StatusInternalServerError)
				render.JSON(w, r, resp.Error("internal error"))
				return
			}

			if time.Now().After(v.ExpiresAt) {
				log.Info("verification code expired", slog.String("email", req.Email))
				_ = store.DeleteEmailVerification(req.Email)
				w.WriteHeader(http.StatusBadRequest)
				render.JSON(w, r, resp.Error("code has expired"))
				return
			}
			verification = v
		}

		userID, err := store.CreateUser(verification.Email, verification.Password)
		if errors.Is(err, storage.ErrUserExists) {
			log.Info("user already exists", slog.String("email", req.Email))
			w.WriteHeader(http.StatusConflict)
			render.JSON(w, r, resp.Error("user already exists"))
			return
		}
		if err != nil {
			log.Error("failed to create user", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to create user"))
			return
		}

		_ = store.DeleteEmailVerification(req.Email)

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id": userID,
			"email":   verification.Email,
			"exp":     time.Now().Add(72 * time.Hour).Unix(),
		})

		tokenStr, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			log.Error("failed to sign token", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to generate token"))
			return
		}

		log.Info("user registered via email verification",
			slog.String("email", verification.Email),
			slog.Int64("user_id", userID),
		)

		render.JSON(w, r, ConfirmResponse{
			Response: resp.OK(),
			Token:    tokenStr,
			Email:    verification.Email,
			UserID:   userID,
		})
	}
}
