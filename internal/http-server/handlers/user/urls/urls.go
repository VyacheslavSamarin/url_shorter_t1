package urls

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

type URLRecord struct {
	ID        int64     `json:"id"`
	Alias     string    `json:"alias"`
	URL       string    `json:"url"`
	ShortURL  string    `json:"short_url"`
	Clicks    int64     `json:"clicks"`
	CreatedAt time.Time `json:"created_at"`
	QRFg      string    `json:"qr_fg"`
	QRBg      string    `json:"qr_bg"`
}

type Response struct {
	resp.Response
	URLs []URLRecord `json:"urls"`
}

type UserURLsGetter interface {
	GetUrlsByUserID(userID int64) ([]postgres.URLRecord, error)
}

func New(log *slog.Logger, getter UserURLsGetter, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.user.urls.New"

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

		records, err := getter.GetUrlsByUserID(userID)
		if err != nil {
			log.Error("failed to get urls", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to get urls"))
			return
		}

		result := make([]URLRecord, 0, len(records))
		for _, rec := range records {
			result = append(result, URLRecord{
				ID:        rec.ID,
				Alias:     rec.Alias,
				URL:       rec.URL,
				ShortURL:  baseURL + "/" + rec.Alias,
				Clicks:    rec.Clicks,
				CreatedAt: rec.CreatedAt,
				QRFg:      rec.QRFg,
				QRBg:      rec.QRBg,
			})
		}

		log.Info("got user urls", slog.Int64("user_id", userID), slog.Int("count", len(result)))

		render.JSON(w, r, Response{
			Response: resp.OK(),
			URLs:     result,
		})
	}
}
