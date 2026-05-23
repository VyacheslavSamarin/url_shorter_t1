package stats

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
	"url-shortener/internal/storage/postgres"

	mw "url-shortener/internal/http-server/middleware"
)

type VisitGetter interface {
	GetVisitsByAlias(alias string) ([]postgres.Visit, error)
	GetUrlsByUserID(userID int64) ([]postgres.URLRecord, error)
}

func New(log *slog.Logger, visitGetter VisitGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.stats.New"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		alias := chi.URLParam(r, "alias")
		if alias == "" {
			log.Info("alias is empty")

			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("invalid request"))
			return
		}

		userID, ok := mw.GetUserID(r)
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			render.JSON(w, r, resp.Error("unauthorized"))
			return
		}

		userURLs, err := visitGetter.GetUrlsByUserID(userID)
		if err != nil {
			log.Error(op, "failed to get user urls", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		ownsAlias := false
		for _, u := range userURLs {
			if u.Alias == alias {
				ownsAlias = true
				break
			}
		}

		if !ownsAlias {
			log.Info("access denied: alias not owned by user",
				slog.String("alias", alias),
				slog.Int64("user_id", userID),
			)
			w.WriteHeader(http.StatusForbidden)
			render.JSON(w, r, resp.Error("access denied"))
			return
		}

		visits, err := visitGetter.GetVisitsByAlias(alias)
		if errors.Is(err, storage.ErrURLNotFound) {
			log.Info("url not found", "alias", alias)

			w.WriteHeader(http.StatusNotFound)
			render.JSON(w, r, resp.Error("url not found"))
			return
		}
		if err != nil {
			log.Error(op, "failed to get visits", sl.Err(err))

			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		log.Info("got visits", slog.Int("count", len(visits)))
		responseOK(w, r, visits)
	}
}

func responseOK(w http.ResponseWriter, r *http.Request, visits []postgres.Visit) {
	render.JSON(w, r, Response{
		Response: resp.OK(),
		Visits:   visits,
	})
}

type Response struct {
	resp.Response
	Visits []postgres.Visit `json:"visits"`
}
