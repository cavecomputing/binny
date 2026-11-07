// Package db is Binny's SQLite database: an index of data/files/, and the things only it knows,
// tags and where trashed items came from. It holds metadata only; the disk is the source of truth.
package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"

	"github.com/cavecomputing/binny/internal/storage"
	_ "modernc.org/sqlite"
)

const schema = `
	-- A row per visible file and folder under data/files/ (see entries.go). path is relative to
	-- data/files/ with "/" separators; parent is '' for the top folder. Folders keep size 0: their
	-- totals are summed from the rows below them.
	CREATE TABLE IF NOT EXISTS entries (
		path TEXT PRIMARY KEY,
		parent TEXT NOT NULL,
		is_dir INTEGER NOT NULL,
		size INTEGER NOT NULL,
		mtime REAL NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_entries_parent ON entries(parent);
	-- What was trashed through the app. name is its name in data/files/.trash/,
	-- "<time_ns>_<original name>"; original is where it came from, so restore can put it back;
	-- size counts everything inside a folder, so the trash never has to walk one.
	CREATE TABLE IF NOT EXISTS trash (
		name TEXT PRIMARY KEY,
		original TEXT NOT NULL,
		size INTEGER NOT NULL
	);
	-- Tags, keyed by path like entries. A trashed item's tags wait under
	-- ".trash/<its name there>" until it is restored or deleted.
	CREATE TABLE IF NOT EXISTS tags (
		path TEXT NOT NULL,
		tag TEXT NOT NULL,
		PRIMARY KEY (path, tag)
	);
	CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
`

// Matches a path and everything below it: bind subtree(path). '0' sorts right after '/', so the
// range is exactly the rows under "path/", and the primary key serves it.
const inSubtree = `(path = ? OR (path > ? AND path < ?))`

func subtree(path string) []any { return []any{path, path + "/", path + "0"} }

type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// conn runs queries on the database or inside a transaction; DB and Tx both have its methods.
type conn struct {
	q    querier
	disk *storage.Disk
}

type DB struct {
	conn
	sql *sql.DB
}

// Tx is a transaction (see DB.Update).
type Tx struct{ conn }

// Open opens the database at path, creating its tables if needed, to index disk.
func Open(path string, disk *storage.Disk) (*DB, error) {
	// WAL and a busy timeout let requests share the file; BEGIN IMMEDIATE takes the write lock up
	// front, so a transaction that reads before it writes waits its turn instead of failing.
	pool, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(schema); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{conn{pool, disk}, pool}, nil
}

func (db *DB) Close() error { return db.sql.Close() }

// Update runs fn in a transaction, committed if fn returns nil and rolled back otherwise.
func (db *DB) Update(fn func(tx *Tx) error) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(&Tx{conn{tx, db.disk}}); err != nil {
		return err
	}
	return tx.Commit()
}

// SecretKey is a random key made the first time it's asked for and kept from then on.
func (db *DB) SecretKey() ([]byte, error) {
	random := make([]byte, 32)
	rand.Read(random)
	var secret string
	err := db.Update(func(tx *Tx) error {
		if _, err := tx.q.Exec(`INSERT OR IGNORE INTO settings (key, value) VALUES ('secret_key', ?)`, hex.EncodeToString(random)); err != nil {
			return err
		}
		return tx.q.QueryRow(`SELECT value FROM settings WHERE key = 'secret_key'`).Scan(&secret)
	})
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(secret)
}

// collect scans each row of a query with scan, then closes the rows.
func collect[T any](rows *sql.Rows, err error, scan func(*sql.Rows) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, item)
	}
	return found, rows.Err()
}
