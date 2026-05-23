package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/joho/godotenv"

	"url-shortener/internal/config"
	authLogin "url-shortener/internal/http-server/handlers/auth/login"
	authLogout "url-shortener/internal/http-server/handlers/auth/logout"
	authRegister "url-shortener/internal/http-server/handlers/auth/register"
	authVerify "url-shortener/internal/http-server/handlers/auth/verify"
	"url-shortener/internal/http-server/handlers/redirect"
	swaggerHandler "url-shortener/internal/http-server/handlers/swagger"
	urlalias "url-shortener/internal/http-server/handlers/url/alias"
	del "url-shortener/internal/http-server/handlers/url/delete"
	urlqr "url-shortener/internal/http-server/handlers/url/qr"
	"url-shortener/internal/http-server/handlers/url/save"
	"url-shortener/internal/http-server/handlers/url/stats"
	userMe "url-shortener/internal/http-server/handlers/user/me"
	userUrls "url-shortener/internal/http-server/handlers/user/urls"
	mwLogger "url-shortener/internal/http-server/middleware"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/handlers/slogpretty"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage/postgres"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found")
	}
}

func main() {
	cfg := config.MustLoad()

	logger := setupLogger(cfg.Env)

	logger.Info("Starting URL Shortener", slog.String("env", cfg.Env))
	logger.Debug("debug messages are enabled")

	storage, err := postgres.New(cfg.DBDsn)
	if err != nil {
		logger.Error("Failed to initialize storage", sl.Err(err))
		os.Exit(1)
	}

	router := chi.NewRouter()

	router.Use(middleware.RequestID)
	router.Use(mwLogger.New(logger))
	router.Use(middleware.Recoverer)
	router.Use(middleware.URLFormat)
	router.Use(corsMiddleware)

	// Swagger UI — доступен по /swagger/
	router.Mount("/swagger", swaggerHandler.Handler())

	router.Post("/auth/register", authRegister.New(logger, storage))
	router.Post("/auth/login", authLogin.New(logger, storage, cfg.JWTSecret))
	router.Post("/auth/logout", authLogout.New(logger, cfg.JWTSecret))
	router.Post("/auth/register/send-code", authVerify.NewSendCode(logger, storage, cfg.SMTP))
	router.Post("/auth/register/verify", authVerify.NewConfirmCode(logger, storage, cfg.JWTSecret, cfg.SkipEmailVerify))

	router.Get("/{alias}", redirect.New(logger, storage))
	router.Get("/{alias}/qr", urlqr.New(logger, storage))

	router.Group(func(r chi.Router) {
		r.Use(mwLogger.OptionalJWTAuth(cfg.JWTSecret))
		r.Post("/url", save.New(logger, storage, cfg.BaseURL))
	})

	router.Group(func(r chi.Router) {
		r.Use(mwLogger.JWTAuth(cfg.JWTSecret))

		r.Get("/stats/{alias}", stats.New(logger, storage))
		r.Get("/user/me", userMe.New(logger, storage))
		r.Get("/user/urls", userUrls.New(logger, storage, cfg.BaseURL))
		r.Put("/{alias}/qr/colors", urlqr.NewColorsHandler(logger, storage))
		r.Put("/{alias}/alias", urlalias.NewUpdateHandler(logger, storage))
		r.Delete("/url/{alias}", del.New(logger, storage))
		r.Delete("/url", func(w http.ResponseWriter, r *http.Request) {
			logger.Info("alias is empty in delete")
			w.WriteHeader(http.StatusBadRequest)
			render.JSON(w, r, resp.Error("alias is required"))
		})
	})

	logger.Info("starting server", slog.String("address", cfg.Address))

	srv := &http.Server{
		Addr:         cfg.Address,
		Handler:      router,
		ReadTimeout:  cfg.HTTPServer.Timeout,
		WriteTimeout: cfg.HTTPServer.Timeout,
		IdleTimeout:  cfg.HTTPServer.IdleTimeout,
	}

	if err := srv.ListenAndServe(); err != nil {
		logger.Error("Failed to start server")
	}

	logger.Error("Stopping server")
}

// corsMiddleware добавляет CORS заголовки для фронтенда
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func setupLogger(env string) *slog.Logger {
	var logger *slog.Logger
	switch env {
	case envLocal:
		logger = setupPrettySlog()
	case envDev:
		logger = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
		)
	case envProd:
		logger = slog.New(
			slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}),
		)
	}

	return logger
}

func setupPrettySlog() *slog.Logger {
	opts := slogpretty.PrettyHandlerOptions{
		SlogOpts: &slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
	}

	handler := opts.NewPrettyHandler(os.Stdout)

	return slog.New(handler)
}
