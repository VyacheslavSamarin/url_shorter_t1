package qr

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

type QRColorsUpdater interface {
	UpdateQRColors(alias string, userID int64, fg, bg string) error
}

type colorsRequest struct {
	FG string `json:"fg"`
	BG string `json:"bg"`
}

var hexRe = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

func NewColorsHandler(log *slog.Logger, updater QRColorsUpdater) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.qr.NewColorsHandler"

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

		alias := chi.URLParam(r, "alias")
		if alias == "" {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("alias is required"))
			return
		}

		var req colorsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid request body"))
			return
		}

		if !hexRe.MatchString(req.FG) {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid fg color, expected 6-char hex without #"))
			return
		}
		if !hexRe.MatchString(req.BG) {
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid bg color, expected 6-char hex without #"))
			return
		}

		err := updater.UpdateQRColors(alias, userID, req.FG, req.BG)
		if errors.Is(err, storage.ErrURLNotFound) {
			w.WriteHeader(http.StatusNotFound)
			render.JSON(w, r, resp.Error("url not found or not owned by user"))
			return
		}
		if err != nil {
			log.Error("failed to update qr colors", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		log.Info("qr colors updated", slog.String("alias", alias), slog.Int64("user_id", userID))
		render.JSON(w, r, resp.OK())
	}
}
