package qr

import (
	"errors"
	"fmt"
	"image/color"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net/http"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"

	"github.com/skip2/go-qrcode"
)

type UrlGetter interface {
	GetUrl(alias string) (string, error)
}

// parseHexColor разбирает hex-цвет вида "#RRGGBB" или "RRGGBB" в color.RGBA
func parseHexColor(s string) (color.RGBA, error) {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid hex color: %s", s)
	}
	r, err := strconv.ParseUint(s[0:2], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	g, err := strconv.ParseUint(s[2:4], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	b, err := strconv.ParseUint(s[4:6], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}

func New(log *slog.Logger, urlGetter UrlGetter, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.qr.New"

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

		_, err := urlGetter.GetUrl(alias)
		if errors.Is(err, storage.ErrURLNotFound) {
			log.Info("url not found", "alias", alias)

			w.WriteHeader(http.StatusNotFound)
			render.JSON(w, r, resp.Error("url not found"))
			return
		}
		if err != nil {
			log.Error(op, "failed to get url", sl.Err(err))

			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		// Формируем сокращённую ссылку для QR-кода
		shortUrl := fmt.Sprintf("%s/%s", baseURL, alias)

		// Читаем query-параметры цвета: fg (foreground) и bg (background)
		fgParam := r.URL.Query().Get("fg")
		bgParam := r.URL.Query().Get("bg")

		fgColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}       // чёрный по умолчанию
		bgColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}  // белый по умолчанию

		if fgParam != "" {
			if parsed, err := parseHexColor(fgParam); err == nil {
				fgColor = parsed
			} else {
				log.Info("invalid fg color, using default", "fg", fgParam)
			}
		}
		if bgParam != "" {
			if parsed, err := parseHexColor(bgParam); err == nil {
				bgColor = parsed
			} else {
				log.Info("invalid bg color, using default", "bg", bgParam)
			}
		}

		// Генерируем QR-код с сокращённой ссылкой
		qr, err := qrcode.New(shortUrl, qrcode.Medium)
		if err != nil {
			log.Error(op, "failed to create qr code", sl.Err(err))

			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to generate qr code"))
			return
		}

		qr.ForegroundColor = fgColor
		qr.BackgroundColor = bgColor

		qrBytes, err := qr.PNG(256)
		if err != nil {
			log.Error(op, "failed to encode qr code", sl.Err(err))

			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("failed to generate qr code"))
			return
		}

		// Устанавливаем правильный Content-Type для изображения
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(qrBytes)
	}
}
