package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type User struct {
	Name             string   `json:"name"`
	Account          string   `json:"account"`
	AccountPublicKey string   `json:"accountPublicKey"`
	Operator         string   `json:"operator"`
	PublicKey        string   `json:"publicKey"`
	PublishAllow     []string `json:"publishAllow"`
}

type AccountSigningKey struct {
	Operator         string
	Account          string
	AccountPublicKey string
	Seed             string
}

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("store: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			operator   TEXT NOT NULL,
			account    TEXT NOT NULL,
			account_public_key TEXT NOT NULL,
			name       TEXT NOT NULL,
			public_key TEXT NOT NULL DEFAULT '',
			seed       TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (operator, account_public_key, name)
		)`,
		`CREATE TABLE IF NOT EXISTS user_publish_allow (
			operator  TEXT NOT NULL,
			account_public_key TEXT NOT NULL,
			user_name TEXT NOT NULL,
			subject   TEXT NOT NULL,
			PRIMARY KEY (operator, account_public_key, user_name, subject)
		)`,
		`CREATE TABLE IF NOT EXISTS account_signing_keys (
			operator           TEXT NOT NULL,
			account            TEXT NOT NULL,
			account_public_key TEXT NOT NULL,
			seed               TEXT NOT NULL,
			PRIMARY KEY (operator, account_public_key)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if !s.hasColumn("users", "account_public_key") || !s.hasColumn("user_publish_allow", "account_public_key") {
		if err := s.resetUserSchema(); err != nil {
			return err
		}
	}
	if !s.hasColumn("users", "seed") {
		if _, err := s.db.Exec("ALTER TABLE users ADD COLUMN seed TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListUsers(operator, accountPublicKey string) ([]User, error) {
	rows, err := s.db.Query(
		"SELECT operator, account, account_public_key, name, public_key FROM users WHERE operator = ? AND account_public_key = ? ORDER BY name",
		operator, accountPublicKey,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Operator, &u.Account, &u.AccountPublicKey, &u.Name, &u.PublicKey); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range users {
		subjects, err := s.listUserPublishAllow(users[i].Operator, users[i].AccountPublicKey, users[i].Name)
		if err != nil {
			return nil, err
		}
		users[i].PublishAllow = subjects
	}
	return users, nil
}

func (s *Store) ListAllUsers() ([]User, error) {
	rows, err := s.db.Query(
		"SELECT operator, account, account_public_key, name, public_key FROM users ORDER BY operator, account, name",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Operator, &u.Account, &u.AccountPublicKey, &u.Name, &u.PublicKey); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range users {
		subjects, err := s.listUserPublishAllow(users[i].Operator, users[i].AccountPublicKey, users[i].Name)
		if err != nil {
			return nil, err
		}
		users[i].PublishAllow = subjects
	}
	return users, nil
}

func (s *Store) GetUser(operator, accountPublicKey, name string) (*User, error) {
	var u User
	err := s.db.QueryRow(
		"SELECT operator, account, account_public_key, name, public_key FROM users WHERE operator = ? AND account_public_key = ? AND name = ?",
		operator, accountPublicKey, name,
	).Scan(&u.Operator, &u.Account, &u.AccountPublicKey, &u.Name, &u.PublicKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	subjects, err := s.listUserPublishAllow(operator, accountPublicKey, name)
	if err != nil {
		return nil, err
	}
	u.PublishAllow = subjects
	return &u, nil
}

func (s *Store) CreateUser(operator, account, accountPublicKey, name, publicKey, seed string) (*User, error) {
	_, err := s.db.Exec(
		"INSERT INTO users (operator, account, account_public_key, name, public_key, seed) VALUES (?, ?, ?, ?, ?, ?)",
		operator, account, accountPublicKey, name, publicKey, seed,
	)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return &User{Operator: operator, Account: account, AccountPublicKey: accountPublicKey, Name: name, PublicKey: publicKey, PublishAllow: []string{}}, nil
}

func (s *Store) SaveAccountSigningKey(operator, account, accountPublicKey, seed string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO account_signing_keys (operator, account, account_public_key, seed) VALUES (?, ?, ?, ?)",
		operator, account, accountPublicKey, seed,
	)
	return err
}

func (s *Store) GetAccountSigningKey(operator, accountPublicKey string) (*AccountSigningKey, error) {
	var key AccountSigningKey
	err := s.db.QueryRow(
		"SELECT operator, account, account_public_key, seed FROM account_signing_keys WHERE operator = ? AND account_public_key = ?",
		operator, accountPublicKey,
	).Scan(&key.Operator, &key.Account, &key.AccountPublicKey, &key.Seed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Store) DeleteAccountSigningKey(operator, accountPublicKey string) error {
	_, err := s.db.Exec(
		"DELETE FROM account_signing_keys WHERE operator = ? AND account_public_key = ?",
		operator, accountPublicKey,
	)
	return err
}

func (s *Store) DeleteAccountData(operator, accountPublicKey string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(
		"DELETE FROM user_publish_allow WHERE operator = ? AND account_public_key = ?",
		operator, accountPublicKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM users WHERE operator = ? AND account_public_key = ?",
		operator, accountPublicKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM account_signing_keys WHERE operator = ? AND account_public_key = ?",
		operator, accountPublicKey,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetUserSeed(operator, accountPublicKey, name string) (string, error) {
	var seed string
	err := s.db.QueryRow(
		"SELECT seed FROM users WHERE operator = ? AND account_public_key = ? AND name = ?",
		operator, accountPublicKey, name,
	).Scan(&seed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return seed, nil
}

func (s *Store) DeleteUser(operator, accountPublicKey, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec(
		"DELETE FROM user_publish_allow WHERE operator = ? AND account_public_key = ? AND user_name = ?",
		operator, accountPublicKey, name,
	); err != nil {
		return err
	}
	res, err := tx.Exec(
		"DELETE FROM users WHERE operator = ? AND account_public_key = ? AND name = ?",
		operator, accountPublicKey, name,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *Store) AddUserPublishAllow(operator, accountPublicKey, user, subject string) (*User, error) {
	if _, err := s.GetUser(operator, accountPublicKey, user); err != nil {
		return nil, err
	}
	_, err := s.db.Exec(
		"INSERT INTO user_publish_allow (operator, account_public_key, user_name, subject) VALUES (?, ?, ?, ?)",
		operator, accountPublicKey, user, subject,
	)
	if isUniqueErr(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.GetUser(operator, accountPublicKey, user)
}

func (s *Store) RemoveUserPublishAllow(operator, accountPublicKey, user, subject string) (*User, error) {
	if _, err := s.GetUser(operator, accountPublicKey, user); err != nil {
		return nil, err
	}
	_, err := s.db.Exec(
		"DELETE FROM user_publish_allow WHERE operator = ? AND account_public_key = ? AND user_name = ? AND subject = ?",
		operator, accountPublicKey, user, subject,
	)
	if err != nil {
		return nil, err
	}
	return s.GetUser(operator, accountPublicKey, user)
}

func (s *Store) listUserPublishAllow(operator, accountPublicKey, user string) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT subject FROM user_publish_allow WHERE operator = ? AND account_public_key = ? AND user_name = ? ORDER BY subject",
		operator, accountPublicKey, user,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	subjects := []string{}
	for rows.Next() {
		var sub string
		if err := rows.Scan(&sub); err != nil {
			return nil, err
		}
		subjects = append(subjects, sub)
	}
	return subjects, rows.Err()
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func (s *Store) hasColumn(table, column string) bool {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false
		}
		if name == column {
			return true
		}
	}
	return false
}

func (s *Store) resetUserSchema() error {
	stmts := []string{
		`DROP TABLE IF EXISTS user_publish_allow`,
		`DROP TABLE IF EXISTS users`,
		`CREATE TABLE users (
			operator   TEXT NOT NULL,
			account    TEXT NOT NULL,
			account_public_key TEXT NOT NULL,
			name       TEXT NOT NULL,
			public_key TEXT NOT NULL DEFAULT '',
			seed       TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (operator, account_public_key, name)
		)`,
		`CREATE TABLE user_publish_allow (
			operator  TEXT NOT NULL,
			account_public_key TEXT NOT NULL,
			user_name TEXT NOT NULL,
			subject   TEXT NOT NULL,
			PRIMARY KEY (operator, account_public_key, user_name, subject)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
