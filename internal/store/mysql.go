package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"microwave-network/internal/domain"
)

type MySQL struct {
	db  *sql.DB
	now func() time.Time
}

func OpenMySQL(dsn string) (*MySQL, error) {
	db, err := sql.Open("mysql", dsn+"?parseTime=true&multiStatements=true")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	return &MySQL{db: db, now: time.Now}, nil
}

func (m *MySQL) Close() error   { return m.db.Close() }
func (m *MySQL) Now() time.Time { return m.now().UTC() }
func (m *MySQL) DB() *sql.DB    { return m.db }

const Schema = `
CREATE TABLE IF NOT EXISTS documents (
  collection VARCHAR(100) NOT NULL,
  id VARCHAR(200) NOT NULL,
  data JSON NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY(collection, id),
  KEY idx_documents_collection (collection)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS id_counters (
  collection VARCHAR(100) PRIMARY KEY,
  value BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
`

func (m *MySQL) Migrate(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, Schema)
	return err
}

type myTx struct{ tx *sql.Tx }

func (m *MySQL) RunTx(ctx context.Context, fn func(Tx) error) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(myTx{tx}); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (m *MySQL) PutDoc(ctx context.Context, collection, id string, value any) error {
	return putDocSQL(ctx, m.db, collection, id, value)
}
func (t myTx) PutDoc(ctx context.Context, collection, id string, value any) error {
	return putDocSQL(ctx, t.tx, collection, id, value)
}

func putDocSQL(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, collection, id string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = ex.ExecContext(ctx, `INSERT INTO documents(collection,id,data,updated_at) VALUES(?,?,?,NOW(6))
		ON DUPLICATE KEY UPDATE data=VALUES(data), updated_at=NOW(6)`, collection, id, string(raw))
	return err
}

func (m *MySQL) GetDoc(ctx context.Context, collection, id string, dst any) error {
	return getDocSQL(ctx, m.db, collection, id, dst)
}
func (t myTx) GetDoc(ctx context.Context, collection, id string, dst any) error {
	return getDocSQL(ctx, t.tx, collection, id, dst)
}
func getDocSQL(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, collection, id string, dst any) error {
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT data FROM documents WHERE collection=? AND id=?`, collection, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func (m *MySQL) DeleteDoc(ctx context.Context, collection, id string) error {
	return deleteDocSQL(ctx, m.db, collection, id)
}
func (t myTx) DeleteDoc(ctx context.Context, collection, id string) error {
	return deleteDocSQL(ctx, t.tx, collection, id)
}
func deleteDocSQL(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, collection, id string) error {
	res, err := ex.ExecContext(ctx, `DELETE FROM documents WHERE collection=? AND id=?`, collection, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (m *MySQL) ListDocs(ctx context.Context, collection string, dst any) error {
	return listDocsSQL(ctx, m.db, collection, dst)
}
func (t myTx) ListDocs(ctx context.Context, collection string, dst any) error {
	return listDocsSQL(ctx, t.tx, collection, dst)
}

type rowQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listDocsSQL(ctx context.Context, q rowQuerier, collection string, dst any) error {
	rows, err := q.QueryContext(ctx, `SELECT data FROM documents WHERE collection=? ORDER BY id`, collection)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return err
		}
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	raw, _ := json.Marshal(items)
	return json.Unmarshal(raw, dst)
}

func (m *MySQL) RevisionOf(ctx context.Context, collection, id string) (int64, error) {
	return revisionOfSQL(ctx, m.db, collection, id)
}
func (t myTx) RevisionOf(ctx context.Context, collection, id string) (int64, error) {
	return revisionOfSQL(ctx, t.tx, collection, id)
}
func revisionOfSQL(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, collection, id string) (int64, error) {
	var v int64
	err := q.QueryRowContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(data,'$.revision')) FROM documents WHERE collection=? AND id=?`, collection, id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	return v, err
}

func (m *MySQL) NextID(ctx context.Context, collection string) (string, error) {
	var id string
	err := m.RunTx(ctx, func(tx Tx) error {
		var err error
		id, err = nextIDSQL(ctx, tx.(myTx).tx, collection)
		return err
	})
	return id, err
}
func (t myTx) NextID(ctx context.Context, collection string) (string, error) {
	return nextIDSQL(ctx, t.tx, collection)
}

type nextQueryer interface {
	rowQuerier
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func nextIDSQL(ctx context.Context, q nextQueryer, collection string) (string, error) {
	// LAST_INSERT_ID(value+1) makes the increment connection-local and avoids a
	// race between UPDATE and SELECT inside a transaction.
	_, err := q.ExecContext(ctx, `INSERT INTO id_counters(collection,value) VALUES(?,0)
		ON DUPLICATE KEY UPDATE value=LAST_INSERT_ID(value+1)`, collection)
	if err != nil {
		return "", err
	}
	var n int64
	if err := q.QueryRowContext(ctx, `SELECT LAST_INSERT_ID()`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", collection, n), nil
}

var _ Store = (*MySQL)(nil)
