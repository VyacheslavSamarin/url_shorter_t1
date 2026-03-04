# URL Shortener (REST API)

Сервис сокращения URL на Go.
Стек: [Chi](https://github.com/go-chi/chi), PostgreSQL, `log/slog`.

## Возможности

- Сокращение URL с автогенерацией алиаса
- Редирект по сокращённой ссылке
- Удаление ссылок по алиасу
- Basic Auth для `POST`/`DELETE`
- Конфигурация через `yaml + env`

## Конфигурация

Нужны переменные окружения:

- `CONFIG_PATH` - путь к YAML-конфигу
- `HTTP_SERVER_PASSWORD` - пароль для Basic Auth
- `DB_DSN` - DSN подключения к PostgreSQL

Пример `DB_DSN`:

`postgres://postgres:postgres@localhost:5432/url_shortener?sslmode=disable`

## Запуск

```bash
export CONFIG_PATH=./config/prod.yaml
export HTTP_SERVER_PASSWORD=mypass
export DB_DSN='postgres://postgres:postgres@localhost:5432/url_shortener?sslmode=disable'
go run ./cmd/url-shortener
```

## Локальный E2E через Docker Compose

1. Поднять PostgreSQL:

```bash
docker compose up -d postgres
```

2. Запустить сервис:

```bash
export CONFIG_PATH=./config/prod.yaml
export HTTP_SERVER_PASSWORD=mypass
export DB_DSN='postgres://postgres:postgres@localhost:5432/url_shortener?sslmode=disable'
go run ./cmd/url-shortener
```

3. Проверка API:

```bash
curl -u Victor_admin:mypass -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","alias":"example"}' \
  http://localhost:8082/url

curl -i http://localhost:8082/example
curl -X DELETE -u Victor_admin:mypass http://localhost:8082/url/example
```

## API

### POST `/url`

```json
{
  "url": "https://example.com",
  "alias": "example"
}
```

`alias` необязателен.

### GET `/{alias}`

Возвращает `302 Found` и редирект на исходный URL.

### DELETE `/url/{alias}`

Удаляет сокращенную ссылку по алиасу.
