package redirect

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
	"url-shortener/internal/storage/postgres"

	"github.com/mssola/useragent"
)

type UrlGetter interface {
	GetUrl(alias string) (string, error)
	SaveVisit(visit postgres.Visit) error
}

func New(log *slog.Logger, urlGetter UrlGetter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.url.redirect.New"

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

		resUrl, err := urlGetter.GetUrl(alias)
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

		visit := collectVisitData(r, alias)

		go func() {
			if err := urlGetter.SaveVisit(visit); err != nil {
				log.Error(op, "failed to save visit", sl.Err(err))
			}
		}()

		log.Info("got url", slog.String("url", resUrl))
		http.Redirect(w, r, resUrl, http.StatusFound)
	}
}

func collectVisitData(r *http.Request, alias string) postgres.Visit {
	ip := getIP(r)
	userAgent := r.Header.Get("User-Agent")

	ua := useragent.New(userAgent)
	browser, _ := ua.Browser()
	deviceType := "desktop"
	if ua.Mobile() {
		deviceType = "mobile"
	}

	referer := r.Header.Get("Referer")

	country, city := lookupGeo(ip)

	return postgres.Visit{
		Alias:      alias,
		IPAddress:  ip,
		UserAgent:  userAgent,
		Referer:    referer,
		Browser:    browser,
		DeviceType: deviceType,
		Country:    country,
		City:       city,
	}
}

type geoResponse struct {
	Status  string `json:"status"`
	Country string `json:"country"`
	City    string `json:"city"`
}

func lookupGeo(ip string) (country, city string) {
	if isPrivateIP(ip) {
		return "Local", ""
	}

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://ip-api.com/json/%s?fields=status,country,city", ip)

	res, err := client.Get(url)
	if err != nil {
		return "", ""
	}
	defer res.Body.Close()

	var geo geoResponse
	if err := json.NewDecoder(res.Body).Decode(&geo); err != nil {
		return "", ""
	}

	if geo.Status != "success" {
		return "", ""
	}

	return geo.Country, geo.City
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true
	}

	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

func getIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
