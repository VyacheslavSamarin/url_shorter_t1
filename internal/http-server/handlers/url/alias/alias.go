package alias

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	mw "url-shortener/internal/http-server/middleware"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
)

type AliasUpdater interface {
	UpdateAlias(oldAlias string, newAlias string, userID int64) error
}

type updateRequest struct {
	NewAlias string `json:"new_alias"`
}

type updateResponse struct {
	resp.Response
	Alias string `json:"alias,omitempty"`
}

// aliasRe — допустимые символы для alias: буквы, цифры, дефис, подчёркивание
var aliasRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func NewUpdateHandler(log *slog.Logger, updater AliasUpdater) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.alias.NewUpdateHandler"

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

		oldAlias := chi.URLParam(r, "alias")
		if oldAlias == "" {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("alias is required"))
			return
		}

		var req updateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid request body"))
			return
		}

		if !aliasRe.MatchString(req.NewAlias) {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid alias: use only letters, digits, - and _, max 64 chars"))
			return
		}

		err := updater.UpdateAlias(oldAlias, req.NewAlias, userID)
		if errors.Is(err, storage.ErrURLExists) {
			w.WriteHeader(http.StatusConflict)
			render.JSON(w, r, resp.Error("alias already taken"))
			return
		}
		if errors.Is(err, storage.ErrURLNotFound) {
			w.WriteHeader(http.StatusNotFound)
			render.JSON(w, r, resp.Error("url not found or not owned by user"))
			return
		}
		if err != nil {
			log.Error("failed to update alias", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		log.Info("alias updated", slog.String("old_alias", oldAlias), slog.String("new_alias", req.NewAlias), slog.Int64("user_id", userID))
		render.JSON(w, r, updateResponse{
			Response: resp.OK(),
			Alias:    req.NewAlias,
		})
	}
}
