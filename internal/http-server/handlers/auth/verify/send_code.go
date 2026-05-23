package verify

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/smtp"
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

func sendEmail(smtpCfg config.SMTPConfig, to, code string) error {
	if smtpCfg.Host == "" || smtpCfg.Username == "" {
		fmt.Printf("[DEV] verification code for %s: %s\n", to, code)
		return nil
	}

	from := smtpCfg.From
	if from == "" {
		from = smtpCfg.Username
	}

	subjectText := "Код подтверждения ShortLinker"
	subjectEncoded := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(subjectText)) + "?="

	body := fmt.Sprintf(
		"Ваш код подтверждения для регистрации в ShortLinker:\n\n%s\n\nКод действителен 15 минут.",
		code,
	)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s",
		from, to, subjectEncoded, base64.StdEncoding.EncodeToString([]byte(body)),
	)

	addr := fmt.Sprintf("%s:%d", smtpCfg.Host, smtpCfg.Port)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}

	client, err := smtp.NewClient(conn, smtpCfg.Host)
	if err != nil {
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Close()

	tlsCfg := &tls.Config{ServerName: smtpCfg.Host}
	if err = client.StartTLS(tlsCfg); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}

	auth := smtp.PlainAuth("", smtpCfg.Username, smtpCfg.Password, smtpCfg.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	wc, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = fmt.Fprint(wc, msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err = wc.Close(); err != nil {
		return fmt.Errorf("smtp close writer: %w", err)
	}

	return client.Quit()
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

		if err := sendEmail(smtpCfg, req.Email, code); err != nil {
			log.Error("failed to send email", sl.Err(err))
		}

		log.Info("verification code sent", slog.String("email", req.Email))

		render.JSON(w, r, SendCodeResponse{Response: resp.OK()})
	}
}
