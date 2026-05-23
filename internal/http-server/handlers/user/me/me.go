package me

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	mw "url-shortener/internal/http-server/middleware"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage/postgres"
)

type UserInfo struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Response struct {
	resp.Response
	User UserInfo `json:"user"`
}

type UserByIDGetter interface {
	GetUserByID(id int64) (*postgres.User, error)
}

func New(log *slog.Logger, getter UserByIDGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.user.me.New"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		userID, ok := mw.GetUserID(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, resp.Error("unauthorized"))
			return
		}

		user, err := getter.GetUserByID(userID)
		if err != nil {
			log.Error("failed to get user", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to get user"))
			return
		}

		render.JSON(w, r, Response{
			Response: resp.OK(),
			User: UserInfo{
				ID:        user.ID,
				Email:     user.Email,
				CreatedAt: user.CreatedAt,
			},
		})
	}
}
