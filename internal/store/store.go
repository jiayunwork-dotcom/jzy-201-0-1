// Package store implements the MySQL 8.0 persistence boundary.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// Store owns the database handle.
type Store struct {
	DB *sql.DB
}

//go:embed schema.sql
var schemaSQL string

// New opens MySQL and creates the schema.
func New(dsn string) (*Store, error) {
	return NewWithDatabase(dsn, "")
}

// NewWithDatabase opens MySQL, issuing an optional initial "USE database" /
// CREATE DATABASE before migrations. The empty database means the DSN already
// selects one.
func NewWithDatabase(dsn, database string) (*Store, error) {
	// Create the database first on a connection without a default schema,
	// then reopen with the database embedded in the DSN so every pooled
	// connection selects it.
	if database != "" {
		cfg, err := mysql.ParseDSN(ensureDSNParams(dsn))
		if err != nil {
			return nil, err
		}
		cfg.DBName = ""
		rootDB, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return nil, err
		}
		if err := rootDB.Ping(); err != nil {
			_ = rootDB.Close()
			return nil, err
		}
		if _, err := rootDB.Exec("CREATE DATABASE IF NOT EXISTS " + database + " CHARACTER SET utf8mb4"); err != nil {
			_ = rootDB.Close()
			return nil, err
		}
		_ = rootDB.Close()
		cfg.DBName = database
		dsn = cfg.FormatDSN()
	} else {
		dsn = ensureDSNParams(dsn)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func ensureDSNParams(dsn string) string {
	if !strings.Contains(dsn, "parseTime=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "parseTime=true&multiStatements=true"
	} else if !strings.Contains(dsn, "multiStatements=") {
		dsn += "&multiStatements=true"
	}
	return dsn
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schemaSQL)
	return err
}

// Close closes the pool.
func (s *Store) Close() error { return s.DB.Close() }

// WithTx executes f inside a transaction.
func (s *Store) WithTx(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// NewID returns a sortable-enough random identifier.
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal[T any](raw []byte) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

func timePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		return &t.Time
	}
	return nil
}

func nullString(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

func nullTimePtr(t *time.Time) sql.NullTime {
	return sql.NullTime{Time: derefTime(t), Valid: t != nil}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
