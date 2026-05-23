package postgres

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"url-shortener/internal/storage"
)

// EmailVerification хранит временный код подтверждения email
type EmailVerification struct {
	ID        int64
	Email     string
	Code      string
	Password  string // хэш пароля, сохраняем до подтверждения
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Storage struct {
	db *sql.DB
}

func New(dsn string) (*Storage, error) {
	const op = "storage.postgres.New"

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Таблица пользователей
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	)`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Таблица ссылок (базовая структура без user_id)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS urls (
		id BIGSERIAL PRIMARY KEY,
		alias TEXT NOT NULL UNIQUE,
		url TEXT NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Миграция: добавляем колонку user_id если её нет
	_, err = db.Exec(`ALTER TABLE urls ADD COLUMN IF NOT EXISTS user_id BIGINT REFERENCES users(id) ON DELETE SET NULL`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Миграция: добавляем колонку created_at если её нет
	_, err = db.Exec(`ALTER TABLE urls ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW()`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Миграция: добавляем колонки цветов QR-кода
	_, err = db.Exec(`ALTER TABLE urls ADD COLUMN IF NOT EXISTS qr_fg TEXT NOT NULL DEFAULT '000000'`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	_, err = db.Exec(`ALTER TABLE urls ADD COLUMN IF NOT EXISTS qr_bg TEXT NOT NULL DEFAULT 'ffffff'`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Создание таблицы для хранения статистики переходов
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS url_visits (
		id BIGSERIAL PRIMARY KEY,
		alias TEXT NOT NULL,
		ip_address INET,
		user_agent TEXT,
		referer TEXT,
		country TEXT,
		city TEXT,
		browser TEXT,
		device_type TEXT,
		created_at TIMESTAMP DEFAULT NOW()
	)`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// Таблица для временных кодов подтверждения email
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS email_verifications (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL,
		code TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT NOW()
	)`)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &Storage{db: db}, nil
}

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

func (s *Storage) CreateUser(email, passwordHash string) (int64, error) {
	const op = "storage.postgres.CreateUser"

	var id int64
	err := s.db.QueryRow(
		"INSERT INTO users(email, password_hash) VALUES($1, $2) RETURNING id",
		email, passwordHash,
	).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, fmt.Errorf("%s: %w", op, storage.ErrUserExists)
		}
		return 0, fmt.Errorf("%s: %w", op, err)
	}

	return id, nil
}

func (s *Storage) GetUserByEmail(email string) (*User, error) {
	const op = "storage.postgres.GetUserByEmail"

	var u User
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, created_at FROM users WHERE email = $1",
		email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &u, nil
}

func (s *Storage) GetUserByID(id int64) (*User, error) {
	const op = "storage.postgres.GetUserByID"

	var u User
	err := s.db.QueryRow(
		"SELECT id, email, password_hash, created_at FROM users WHERE id = $1",
		id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrUserNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &u, nil
}

type URLRecord struct {
	ID        int64
	Alias     string
	URL       string
	UserID    *int64
	CreatedAt time.Time
	Clicks    int64
	QRFg      string // hex без #, например "000000"
	QRBg      string // hex без #, например "ffffff"
}

func (s *Storage) SaveUrl(urlToSave string, alias string) error {
	return s.SaveUrlForUser(urlToSave, alias, nil)
}

func (s *Storage) SaveUrlForUser(urlToSave string, alias string, userID *int64) error {
	const op = "storage.postgres.SaveURL"

	var err error
	if userID != nil {
		_, err = s.db.Exec(
			"INSERT INTO urls(url, alias, user_id) VALUES($1, $2, $3)",
			urlToSave, alias, *userID,
		)
	} else {
		_, err = s.db.Exec(
			"INSERT INTO urls(url, alias) VALUES($1, $2)",
			urlToSave, alias,
		)
	}

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%s: %w", op, storage.ErrURLExists)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Storage) GetUrl(alias string) (string, error) {
	const op = "storage.postgres.GetUrl"

	stmt, err := s.db.Prepare("SELECT url FROM urls WHERE alias = $1")
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}
	defer stmt.Close()

	var resURL string

	err = stmt.QueryRow(alias).Scan(&resURL)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%s: %w", op, storage.ErrURLNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", op, err)
	}

	return resURL, nil
}

func (s *Storage) GetUrlsByUserID(userID int64) ([]URLRecord, error) {
	const op = "storage.postgres.GetUrlsByUserID"

	rows, err := s.db.Query(`
		SELECT u.id, u.alias, u.url, u.user_id, u.created_at,
		       COUNT(v.id) as clicks,
		       u.qr_fg, u.qr_bg
		FROM urls u
		LEFT JOIN url_visits v ON v.alias = u.alias
		WHERE u.user_id = $1
		GROUP BY u.id, u.alias, u.url, u.user_id, u.created_at, u.qr_fg, u.qr_bg
		ORDER BY u.created_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var records []URLRecord
	for rows.Next() {
		var rec URLRecord
		err := rows.Scan(&rec.ID, &rec.Alias, &rec.URL, &rec.UserID, &rec.CreatedAt, &rec.Clicks, &rec.QRFg, &rec.QRBg)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		records = append(records, rec)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return records, nil
}

func (s *Storage) UpdateAlias(oldAlias string, newAlias string, userID int64) error {
	const op = "storage.postgres.UpdateAlias"

	// Проверяем что новый alias не занят
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM urls WHERE alias = $1", newAlias).Scan(&count)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if count > 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrURLExists)
	}

	res, err := s.db.Exec(
		`UPDATE urls SET alias = $1 WHERE alias = $2 AND user_id = $3`,
		newAlias, oldAlias, userID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%s: %w", op, storage.ErrURLExists)
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrURLNotFound)
	}

	return nil
}

func (s *Storage) UpdateQRColors(alias string, userID int64, fg, bg string) error {
	const op = "storage.postgres.UpdateQRColors"

	res, err := s.db.Exec(
		`UPDATE urls SET qr_fg = $1, qr_bg = $2 WHERE alias = $3 AND user_id = $4`,
		fg, bg, alias, userID,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrURLNotFound)
	}

	return nil
}

func (s *Storage) DeleteUrl(alias string) error {
	const op = "storage.postgres.DeleteURL"

	stmt, err := s.db.Prepare("DELETE FROM urls WHERE alias = $1")
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(alias)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("%s: %w", op, storage.ErrUrlDeleted)
	}

	return nil
}

type Visit struct {
	ID         int64  `json:"id"`
	Alias      string `json:"alias"`
	IPAddress  string `json:"ip_address"`
	UserAgent  string `json:"user_agent"`
	Referer    string `json:"referer"`
	Country    string `json:"country"`
	City       string `json:"city"`
	Browser    string `json:"browser"`
	DeviceType string `json:"device_type"`
	CreatedAt  string `json:"created_at"`
}

func (s *Storage) SaveVisit(visit Visit) error {
	const op = "storage.postgres.SaveVisit"

	stmt, err := s.db.Prepare("INSERT INTO url_visits(alias, ip_address, user_agent, referer, country, city, browser, device_type) VALUES($1, $2, $3, $4, $5, $6, $7, $8)")
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(visit.Alias, visit.IPAddress, visit.UserAgent, visit.Referer, visit.Country, visit.City, visit.Browser, visit.DeviceType)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Storage) GetVisitsByAlias(alias string) ([]Visit, error) {
	const op = "storage.postgres.GetVisitsByAlias"

	stmt, err := s.db.Prepare("SELECT id, alias, ip_address, user_agent, referer, country, city, browser, device_type, created_at FROM url_visits WHERE alias = $1 ORDER BY created_at DESC")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer stmt.Close()

	rows, err := stmt.Query(alias)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer rows.Close()

	var visits []Visit
	for rows.Next() {
		var v Visit
		err := rows.Scan(&v.ID, &v.Alias, &v.IPAddress, &v.UserAgent, &v.Referer, &v.Country, &v.City, &v.Browser, &v.DeviceType, &v.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		visits = append(visits, v)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return visits, nil
}

func (s *Storage) SaveEmailVerification(email, code, passwordHash string, expiresAt time.Time) error {
	const op = "storage.postgres.SaveEmailVerification"

	// Удаляем старые коды для этого email
	_, err := s.db.Exec("DELETE FROM email_verifications WHERE email = $1", email)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	_, err = s.db.Exec(
		"INSERT INTO email_verifications(email, code, password_hash, expires_at) VALUES($1, $2, $3, $4)",
		email, code, passwordHash, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (s *Storage) GetEmailVerification(email, code string) (*EmailVerification, error) {
	const op = "storage.postgres.GetEmailVerification"

	var v EmailVerification
	err := s.db.QueryRow(
		"SELECT id, email, code, password_hash, expires_at, created_at FROM email_verifications WHERE email = $1 AND code = $2",
		email, code,
	).Scan(&v.ID, &v.Email, &v.Code, &v.Password, &v.ExpiresAt, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrVerificationNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &v, nil
}

func (s *Storage) GetEmailVerificationByEmail(email string) (*EmailVerification, error) {
	const op = "storage.postgres.GetEmailVerificationByEmail"

	var v EmailVerification
	err := s.db.QueryRow(
		"SELECT id, email, code, password_hash, expires_at, created_at FROM email_verifications WHERE email = $1 ORDER BY created_at DESC LIMIT 1",
		email,
	).Scan(&v.ID, &v.Email, &v.Code, &v.Password, &v.ExpiresAt, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", op, storage.ErrVerificationNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	return &v, nil
}

func (s *Storage) DeleteEmailVerification(email string) error {
	const op = "storage.postgres.DeleteEmailVerification"

	_, err := s.db.Exec("DELETE FROM email_verifications WHERE email = $1", email)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}
