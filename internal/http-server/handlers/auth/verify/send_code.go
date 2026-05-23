package verify

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
	"golang.org/x/crypto/bcrypt"

	"url-shortener/internal/config"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage"
)

type SendCodeRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
}

type SendCodeResponse struct {
	resp.Response
}

type VerificationSaver interface {
	SaveEmailVerification(email, code, passwordHash string, expiresAt time.Time) error
}

type UserChecker interface {
	CreateUser(email, passwordHash string) (int64, error)
}

func generateCode() (string, error) {
	const digits = "0123456789"
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		if err != nil {
			return "", err
		}
		code[i] = digits[n.Int64()]
	}
	return string(code), nil
}

// sendEmailResend отправляет письмо через Resend HTTP API
func sendEmailResend(apiKey, from, to, code string) error {
	type emailPayload struct {
		From    string `json:"from"`
		To      []string `json:"to"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}

	payload := emailPayload{
		From:    from,
		To:      []string{to},
		Subject: "Код подтверждения ShortLinker",
		Text: fmt.Sprintf(
			"Ваш код подтверждения для регистрации в ShortLinker:\n\n%s\n\nКод действителен 15 минут.",
			code,
		),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("resend marshal: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("resend do request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		var errBody map[string]interface{}
		json.NewDecoder(res.Body).Decode(&errBody)
		return fmt.Errorf("resend api error %d: %v", res.StatusCode, errBody)
	}

	return nil
}

func sendEmail(smtpCfg config.SMTPConfig, to, code string) error {
	// Если задан Resend API ключ — используем его
	if smtpCfg.ResendAPIKey != "" {
		from := smtpCfg.From
		if from == "" {
			from = "onboarding@resend.dev"
		}
		return sendEmailResend(smtpCfg.ResendAPIKey, from, to, code)
	}

	// Fallback: вывод в лог для dev окружения
	fmt.Printf("[DEV] verification code for %s: %s\n", to, code)
	return nil
}

func NewSendCode(log *slog.Logger, saver VerificationSaver, smtpCfg config.SMTPConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const op = "handlers.auth.verify.NewSendCode"

		log = log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		var req SendCodeRequest
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

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Error("failed to hash password", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		code, err := generateCode()
		if err != nil {
			log.Error("failed to generate code", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		expiresAt := time.Now().Add(15 * time.Minute)

		if err := saver.SaveEmailVerification(req.Email, code, string(hash), expiresAt); err != nil {
			if errors.Is(err, storage.ErrUserExists) {
				w.WriteHeader(http.StatusConflict)
				render.JSON(w, r, resp.Error("user already exists"))
				return
			}
			log.Error("failed to save verification", sl.Err(err))
			w.WriteHeader(http.StatusInternalServerError)
			render.JSON(w, r, resp.Error("internal error"))
			return
		}

		go func() {
			if err := sendEmail(smtpCfg, req.Email, code); err != nil {
				log.Error("failed to send email", sl.Err(err))
			} else {
				log.Info("email sent successfully", slog.String("email", req.Email))
			}
		}()

		log.Info("verification code queued", slog.String("email", req.Email))

		render.JSON(w, r, SendCodeResponse{Response: resp.OK()})
	}
}
