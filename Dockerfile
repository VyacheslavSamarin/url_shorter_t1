# ── Стадия сборки ─────────────────────────────────────────────────────────────
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Копируем зависимости отдельно для кэширования слоёв
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники и собираем бинарник
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o url-shortener ./cmd/url-shortener/main.go

# ── Финальный образ ───────────────────────────────────────────────────────────
FROM alpine:3.19

# Сертификаты нужны для HTTPS-запросов (ip-api.com, SMTP)
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/url-shortener .

# Render передаёт PORT через переменную окружения
# Приложение слушает на HTTP_SERVER_ADDRESS, поэтому используем env-default
EXPOSE 8082

CMD ["./url-shortener"]
