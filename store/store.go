// Package store keeps the history that the public APIs forget: DART's
// company list as it changes, and ownership reports that OpenDART stops
// returning after about two years or replaces with a correction. It is one
// SQLite file, written by cmd/korea-data-collect. See docs/ENTITY_DESIGN.md.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure Go, so release builds stay CGO_ENABLED=0
)

// The schema is kept to what SQLite and PostgreSQL both accept, so a hosted
// version can move to Postgres with the same tables.
const schema = `
CREATE TABLE IF NOT EXISTS corp (
  corp_code   TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  eng_name    TEXT NOT NULL,
  stock_code  TEXT NOT NULL,
  modify_date TEXT NOT NULL,
  first_seen  TEXT NOT NULL,
  last_seen   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS corp_event (
  corp_code   TEXT NOT NULL,
  type        TEXT NOT NULL,
  seen_on     TEXT NOT NULL,
  old_value   TEXT NOT NULL,
  new_value   TEXT NOT NULL,
  modify_date TEXT NOT NULL,
  PRIMARY KEY (corp_code, type, seen_on)
);

CREATE TABLE IF NOT EXISTS ownership_report (
  kind       TEXT NOT NULL,
  rcept_no   TEXT NOT NULL,
  corp_code  TEXT NOT NULL,
  reporter   TEXT NOT NULL,
  filed_on   TEXT NOT NULL,
  body       TEXT NOT NULL,
  first_seen TEXT NOT NULL,
  PRIMARY KEY (kind, rcept_no)
);
CREATE INDEX IF NOT EXISTS ownership_report_corp ON ownership_report (corp_code, kind, filed_on);

CREATE TABLE IF NOT EXISTS corp_sync (
  corp_code TEXT NOT NULL,
  kind      TEXT NOT NULL,
  synced_at TEXT NOT NULL,
  PRIMARY KEY (corp_code, kind)
);

CREATE TABLE IF NOT EXISTS run (
  id          INTEGER PRIMARY KEY,
  mode        TEXT NOT NULL,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  status      TEXT NOT NULL,
  stats       TEXT,
  error       TEXT
);
`

// Event types recorded when the company list changes.
const (
	EventAdded            = "added"
	EventRemoved          = "removed"
	EventRenamed          = "renamed"
	EventListed           = "listed"
	EventDelisted         = "delisted"
	EventStockCodeChanged = "stock_code_changed"
)

type Store struct {
	db *sql.DB
}

// Open opens or creates the database at path and applies the schema.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; SQLite serializes writes anyway
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Corp is a company as last seen in DART's company list.
type Corp struct {
	CorpCode   string
	Name       string
	EngName    string
	StockCode  string
	ModifyDate string
}

// Event is a change between two company lists.
type Event struct {
	CorpCode   string
	Type       string
	SeenOn     string
	OldValue   string
	NewValue   string
	ModifyDate string
}

// CorpCount returns how many companies are stored, removed ones included.
func (s *Store) CorpCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corp`).Scan(&n)
	return n, err
}

// ErrShrunk is returned when a new company list is much shorter than the
// stored one: more likely a truncated download than mass deregistration.
var ErrShrunk = errors.New("store: company list shrank by more than 10%; not applied")

// SyncCorps applies today's company list. On the first sync it only stores
// the list; afterwards it records an event for each difference. Companies
// missing from the list are marked removed once, and kept.
func (s *Store) SyncCorps(ctx context.Context, corps []Corp, today string) ([]Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	old := map[string]Corp{}
	present := map[string]bool{} // seen in the latest list stored
	var lastSeen string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(last_seen), '') FROM corp`).Scan(&lastSeen); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT corp_code, name, eng_name, stock_code, modify_date, last_seen FROM corp`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c Corp
		var seen string
		if err := rows.Scan(&c.CorpCode, &c.Name, &c.EngName, &c.StockCode, &c.ModifyDate, &seen); err != nil {
			rows.Close()
			return nil, err
		}
		old[c.CorpCode] = c
		present[c.CorpCode] = seen == lastSeen
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	first := len(old) == 0
	var live int
	for _, ok := range present {
		if ok {
			live++
		}
	}
	if !first && len(corps) < live*9/10 {
		return nil, fmt.Errorf("%w (%d stored, %d new)", ErrShrunk, live, len(corps))
	}

	upsert, err := tx.PrepareContext(ctx, `
INSERT INTO corp (corp_code, name, eng_name, stock_code, modify_date, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (corp_code) DO UPDATE SET name = excluded.name, eng_name = excluded.eng_name,
  stock_code = excluded.stock_code, modify_date = excluded.modify_date, last_seen = excluded.last_seen`)
	if err != nil {
		return nil, err
	}
	defer upsert.Close()

	var events []Event
	add := func(c Corp, typ, oldV, newV string) {
		events = append(events, Event{CorpCode: c.CorpCode, Type: typ, SeenOn: today, OldValue: oldV, NewValue: newV, ModifyDate: c.ModifyDate})
	}
	seen := make(map[string]bool, len(corps))
	for _, c := range corps {
		if seen[c.CorpCode] {
			continue
		}
		seen[c.CorpCode] = true
		if !first {
			o, ok := old[c.CorpCode]
			switch {
			case !ok:
				add(c, EventAdded, "", c.Name)
			default:
				if !present[c.CorpCode] {
					add(c, EventAdded, "", c.Name) // back after being removed
				}
				if o.Name != c.Name {
					add(c, EventRenamed, o.Name, c.Name)
				}
				switch {
				case o.StockCode == "" && c.StockCode != "":
					add(c, EventListed, "", c.StockCode)
				case o.StockCode != "" && c.StockCode == "":
					add(c, EventDelisted, o.StockCode, "")
				case o.StockCode != c.StockCode:
					add(c, EventStockCodeChanged, o.StockCode, c.StockCode)
				}
			}
		}
		if _, err := upsert.ExecContext(ctx, c.CorpCode, c.Name, c.EngName, c.StockCode, c.ModifyDate, today, today); err != nil {
			return nil, err
		}
	}
	if !first {
		for code, o := range old {
			if present[code] && !seen[code] {
				add(o, EventRemoved, o.Name, "")
			}
		}
	}

	ins, err := tx.PrepareContext(ctx, `
INSERT INTO corp_event (corp_code, type, seen_on, old_value, new_value, modify_date)
VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return nil, err
	}
	defer ins.Close()
	for _, e := range events {
		if _, err := ins.ExecContext(ctx, e.CorpCode, e.Type, e.SeenOn, e.OldValue, e.NewValue, e.ModifyDate); err != nil {
			return nil, err
		}
	}
	return events, tx.Commit()
}

// Events returns company list events seen on or after since, oldest first.
func (s *Store) Events(ctx context.Context, since string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT corp_code, type, seen_on, old_value, new_value, modify_date FROM corp_event
WHERE seen_on >= ? ORDER BY seen_on, corp_code, type`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.CorpCode, &e.Type, &e.SeenOn, &e.OldValue, &e.NewValue, &e.ModifyDate); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListedCorps returns listed companies, the ones never synced for kind first,
// then the longest ago.
func (s *Store) ListedCorps(ctx context.Context, kind string, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.corp_code FROM corp c
LEFT JOIN corp_sync s ON s.corp_code = c.corp_code AND s.kind = ?
WHERE c.stock_code <> '' AND c.last_seen = (SELECT MAX(last_seen) FROM corp)
ORDER BY COALESCE(s.synced_at, ''), c.corp_code
LIMIT ?`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// Report is one ownership report, stored as the tool returned it.
type Report struct {
	Kind      string // major_holders | insiders
	ReceiptNo string
	CorpCode  string
	Reporter  string
	FiledOn   string
	Body      json.RawMessage
}

// AddReports stores reports not stored before and marks the company synced
// for kind. Reports are keyed by receipt number: a correction is a new
// report, and the original stays even after DART stops returning it.
func (s *Store) AddReports(ctx context.Context, corpCode, kind string, reports []Report, now time.Time) (added int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	ins, err := tx.PrepareContext(ctx, `
INSERT INTO ownership_report (kind, rcept_no, corp_code, reporter, filed_on, body, first_seen)
VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer ins.Close()
	ts := now.UTC().Format(time.RFC3339)
	for _, r := range reports {
		res, err := ins.ExecContext(ctx, r.Kind, r.ReceiptNo, r.CorpCode, r.Reporter, r.FiledOn, string(r.Body), ts)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		added += int(n)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO corp_sync (corp_code, kind, synced_at) VALUES (?, ?, ?)
ON CONFLICT (corp_code, kind) DO UPDATE SET synced_at = excluded.synced_at`, corpCode, kind, ts); err != nil {
		return 0, err
	}
	return added, tx.Commit()
}

// ReportCount returns how many reports of kind are stored for a company,
// or for all companies when corpCode is empty.
func (s *Store) ReportCount(ctx context.Context, corpCode, kind string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM ownership_report WHERE kind = ? AND (? = '' OR corp_code = ?)`, kind, corpCode, corpCode).Scan(&n)
	return n, err
}

// StartRun records the start of a collector run and returns its ID.
func (s *Store) StartRun(ctx context.Context, mode string, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO run (mode, started_at, status) VALUES (?, ?, 'running')`,
		mode, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishRun records how a run ended: ok, partial (stopped early, for example
// at the daily quota) or failed.
func (s *Store) FinishRun(ctx context.Context, id int64, status string, stats any, runErr error, now time.Time) error {
	b, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	var msg *string
	if runErr != nil {
		msg = new(runErr.Error())
	}
	_, err = s.db.ExecContext(ctx, `UPDATE run SET finished_at = ?, status = ?, stats = ?, error = ? WHERE id = ?`,
		now.UTC().Format(time.RFC3339), status, string(b), msg, id)
	return err
}
