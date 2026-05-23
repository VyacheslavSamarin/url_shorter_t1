package config

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Env               string `yaml:"env" env:"ENV" env-default:"prod"`
	DBDsn             string `yaml:"db_dsn" env:"DB_DSN" env-required:"true"`
	JWTSecret         string `yaml:"jwt_secret" env:"JWT_SECRET" env-default:"change-me-in-production"`
	BaseURL           string `yaml:"base_url" env:"BASE_URL" env-default:"http://localhost:8082"`
	SkipEmailVerify   bool   `yaml:"skip_email_verify" env:"SKIP_EMAIL_VERIFY" env-default:"false"`
	HTTPServer        `yaml:"http_server"`
	SMTP              SMTPConfig `yaml:"smtp"`
}

type SMTPConfig struct {
	Host     string `yaml:"host" env:"SMTP_HOST" env-default:"smtp.gmail.com"`
	Port     int    `yaml:"port" env:"SMTP_PORT" env-default:"587"`
	Username string `yaml:"username" env:"SMTP_USERNAME"`
	Password string `yaml:"password" env:"SMTP_PASSWORD"`
	From     string `yaml:"from" env:"SMTP_FROM"`
}

type HTTPServer struct {
	Address     string        `yaml:"address" env:"HTTP_SERVER_ADDRESS" env-default:"0.0.0.0:8082"`
	Timeout     time.Duration `yaml:"timeout" env:"HTTP_SERVER_TIMEOUT" env-default:"4s"`
	IdleTimeout time.Duration `yaml:"idle_timeout" env:"HTTP_SERVER_IDLE_TIMEOUT" env-default:"30s"`
	Username    string        `yaml:"username" env:"HTTP_SERVER_USERNAME" env-default:"admin"`
	Password    string        `yaml:"password" env:"HTTP_SERVER_PASSWORD" env-default:""`
}

func MustLoad() *Config {
	configPath := os.Getenv("CONFIG_PATH")

	// Railway предоставляет DATABASE_URL — используем как fallback для DB_DSN
	if os.Getenv("DB_DSN") == "" {
		if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
			os.Setenv("DB_DSN", dbURL)
		}
	}

	var cfg Config

	if configPath != "" {
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			log.Fatalf("config file doesn't exist: %s", configPath)
		}
		if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
			log.Fatalf("cannot read config: %s", err)
		}
	} else {
		// Без yaml-файла — читаем только из переменных окружения
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			log.Fatalf("cannot read env config: %s", err)
		}
	}

	if port := os.Getenv("PORT"); port != "" {
		cfg.HTTPServer.Address = fmt.Sprintf("0.0.0.0:%s", port)
	}

	return &cfg
}
