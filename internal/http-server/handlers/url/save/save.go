package save

import (
	"errors"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"log/slog"
	"net/http"
	mw "url-shortener/internal/http-server/middleware"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/lib/random"
	"url-shortener/internal/storage"

	resp "url-shortener/internal/lib/api/response"
)

type Request struct {
	Url   string `json:"url" validate:"required,url"`
	Alias string `json:"alias,omitempty"`
}

type Response struct {
	resp.Response
	Alias    string `json:"alias,omitempty"`
	ShortURL string `json:"short_url,omitempty"`
}

const aliasLen = 6

type UrlSaver interface {
	SaveUrlForUser(urlToSave string, alias string, userID *int64) error
}

func New(log *slog.Logger, urlSaver UrlSaver, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.save.New"

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

		log.Info("request body decoded", slog.Any("req", req))

		if err := validator.New().Struct(req); err != nil {
			validateErr := err.(validator.ValidationErrors)
			log.Error("invalid request", sl.Err(err))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.ValidationError(validateErr))

			return
		}

		alias := req.Alias
		if alias == "" {
			alias = random.NewRandomString(aliasLen)
		}

		var userIDPtr *int64
		if userID, ok := mw.GetUserID(r); ok {
			userIDPtr = &userID
		}

		err := urlSaver.SaveUrlForUser(req.Url, alias, userIDPtr)
		if errors.Is(err, storage.ErrURLExists) {
			log.Info("url already exists", slog.String("url", req.Url))

			w.WriteHeader(http.StatusConflict)
			render.JSON(w, r, resp.Error("alias already exists"))

			return
		}
		if err != nil {
			log.Error("failed to add url", sl.Err(err))

			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to add url"))

			return
		}

		log.Info("url added", slog.String("url", req.Url))

		render.JSON(w, r, Response{
			Response: resp.OK(),
			Alias:    alias,
			ShortURL: baseURL + "/" + alias,
		})
	}
}
