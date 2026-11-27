package sessionusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"modernc.org/sqlite"
)

const indexStoreVersion = 1

// This is a disposable metadata cache, separate from the proxy request ledger.
// Connections live for one refresh; closing them releases native page caches.
type indexStore struct {
	db *sql.DB
}

func indexStorePath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".gob") {
		return strings.TrimSuffix(path, filepath.Ext(path)) + ".sqlite"
	}
	if strings.EqualFold(filepath.Ext(path), ".sqlite") {
		return path
	}
	return path + ".sqlite"
}

func openIndexStore(ctx context.Context, path string) (*indexStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(2000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2048)&_pragma=temp_store(FILE)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &indexStore{db: db}
	if err := s.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func isCorruptIndex(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code() & 0xff
	return code == 11 || code == 26 // SQLITE_CORRUPT / SQLITE_NOTADB
}

// Replace only a confirmed corrupt disposable cache. IO/permission errors and
// unsupported newer versions keep their original files and use source fallback.
func rebuildCorruptIndex(ctx context.Context, path string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".local-usage-rebuild-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	defer func() {
		for _, file := range []string{name, name + "-wal", name + "-shm"} {
			_ = os.Remove(file)
		}
	}()
	s, err := openIndexStore(ctx, name)
	if err != nil {
		return err
	}
	if err := s.db.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (s *indexStore) initialize(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != indexStoreVersion {
		return fmt.Errorf("unsupported local usage cache version %d", version)
	}
	_, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS files (
			file_key TEXT PRIMARY KEY, provider TEXT NOT NULL, title TEXT NOT NULL,
			size INTEGER NOT NULL, modified_ns INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS calls (
			file_key TEXT NOT NULL REFERENCES files(file_key) ON DELETE CASCADE,
			id TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL, model_fold TEXT NOT NULL,
			session TEXT NOT NULL, project TEXT NOT NULL, at_ns INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL, output_tokens INTEGER NOT NULL,
			cache_read INTEGER NOT NULL, cache_write INTEGER NOT NULL, cache_write_1h INTEGER NOT NULL,
			PRIMARY KEY (file_key, id)
		);
		CREATE INDEX IF NOT EXISTS calls_range ON calls(provider, at_ns);
		CREATE INDEX IF NOT EXISTS calls_identity ON calls(id, at_ns DESC, file_key);
		CREATE TABLE IF NOT EXISTS activity (
			file_key TEXT NOT NULL REFERENCES files(file_key) ON DELETE CASCADE,
			activity_key TEXT NOT NULL, provider TEXT NOT NULL, model TEXT NOT NULL, model_fold TEXT NOT NULL,
			session TEXT NOT NULL, project TEXT NOT NULL, at_ns INTEGER NOT NULL,
			milliseconds INTEGER NOT NULL, prompts INTEGER NOT NULL,
			PRIMARY KEY (file_key, activity_key)
		);
		CREATE INDEX IF NOT EXISTS activity_range ON activity(provider, at_ns);
		CREATE INDEX IF NOT EXISTS activity_identity ON activity(activity_key, at_ns DESC, file_key);
		CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		PRAGMA user_version = 1;
	`)
	return err
}

func (s *indexStore) manifest(ctx context.Context) (map[string]indexedFile, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT file_key, provider, title, size, modified_ns FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]indexedFile)
	for rows.Next() {
		var key string
		var file indexedFile
		var modified int64
		if err := rows.Scan(&key, &file.Provider, &file.Title, &file.Size, &modified); err != nil {
			return nil, err
		}
		file.Modified = time.Unix(0, modified)
		result[key] = file
	}
	return result, rows.Err()
}

func (s *indexStore) importedLegacy(ctx context.Context) (bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key = 'legacy_imported'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return value == "1", err
}

func (s *indexStore) markLegacyImported(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT OR REPLACE INTO metadata(key, value) VALUES ('legacy_imported', '1')")
	return err
}

const storeCallSQL = `INSERT OR REPLACE INTO calls
	(file_key, id, provider, model, model_fold, session, project, at_ns, input_tokens, output_tokens, cache_read, cache_write, cache_write_1h)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const storeActivitySQL = `INSERT OR REPLACE INTO activity
	(file_key, activity_key, provider, model, model_fold, session, project, at_ns, milliseconds, prompts)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// SQLite lower() handles ASCII only; preserve Reader's Unicode EqualFold
// behavior by indexing the smallest rune in each simple-fold equivalence class.
func foldModel(value string) string {
	var result strings.Builder
	for _, r := range value {
		folded := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			folded = min(folded, next)
		}
		result.WriteRune(folded)
	}
	return result.String()
}

// Full replacement is needed only for a new/replaced transcript or a parser
// restored without continuation state. Live appends write changed IDs only.
func (s *indexStore) saveFile(ctx context.Context, key string, file indexedFile, p *parser, replace bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO files(file_key, provider, title, size, modified_ns) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(file_key) DO UPDATE SET provider=excluded.provider, title=excluded.title, size=excluded.size, modified_ns=excluded.modified_ns`,
		key, file.Provider, file.Title, file.Size, file.Modified.UnixNano())
	if err != nil {
		return err
	}
	if replace {
		if _, err := tx.ExecContext(ctx, "DELETE FROM calls WHERE file_key = ?", key); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM activity WHERE file_key = ?", key); err != nil {
			return err
		}
	}
	callStmt, err := tx.PrepareContext(ctx, storeCallSQL)
	if err != nil {
		return err
	}
	defer callStmt.Close()
	putCall := func(id string) error {
		item, found := p.calls[id]
		if !found {
			_, err := tx.ExecContext(ctx, "DELETE FROM calls WHERE file_key = ? AND id = ?", key, id)
			return err
		}
		_, err := callStmt.ExecContext(ctx, key, item.ID, item.Provider, item.Model, foldModel(item.Model), item.Session, item.Project, item.At.UnixNano(),
			item.Input, item.Output, item.CacheRead, item.CacheWrite, item.CacheWrite1h)
		return err
	}
	if replace {
		for id := range p.calls {
			if err := putCall(id); err != nil {
				return err
			}
		}
	} else {
		for id := range p.changedCalls {
			if err := putCall(id); err != nil {
				return err
			}
		}
	}
	activityStmt, err := tx.PrepareContext(ctx, storeActivitySQL)
	if err != nil {
		return err
	}
	defer activityStmt.Close()
	putActivity := func(id string) error {
		item := p.activity[id]
		_, err := activityStmt.ExecContext(ctx, key, id, item.Provider, item.Model, foldModel(item.Model), item.Session, item.Project, item.At.UnixNano(), item.Milliseconds, item.Prompts)
		return err
	}
	if replace {
		for id := range p.activity {
			if err := putActivity(id); err != nil {
				return err
			}
		}
	} else {
		for id := range p.changedActivity {
			if err := putActivity(id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *indexStore) prune(ctx context.Context, manifest map[string]indexedFile, seen map[string]bool, provider string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, file := range manifest {
		if (provider == "" || provider == file.Provider) && !seen[key] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE file_key = ?", key); err != nil {
				return err
			}
			delete(manifest, key)
		}
	}
	return tx.Commit()
}

// Titles repeat on thousands of calls in long sessions. Read each file's title
// once and return its integer identity in the row stream instead of allocating
// another title string for every call and activity bucket.
func (s *indexStore) fileTitles(ctx context.Context) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT rowid, title FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := make(map[int64]string)
	for rows.Next() {
		var id int64
		var title string
		if err := rows.Scan(&id, &title); err != nil {
			return nil, err
		}
		titles[id] = title
	}
	return titles, rows.Err()
}

func (s *indexStore) visit(ctx context.Context, start, end time.Time, provider, model string, call func(Call), activity func(Activity)) error {
	if call == nil && activity == nil {
		return ctx.Err()
	}
	titles, err := s.fileTitles(ctx)
	if err != nil {
		return err
	}
	filter := " WHERE c.at_ns >= ? AND c.at_ns < ?"
	args := []any{start.UnixNano(), end.UnixNano()}
	if provider != "" {
		filter += " AND c.provider = ?"
		args = append(args, provider)
	}
	if model != "" {
		filter += " AND c.model_fold = ?"
		args = append(args, foldModel(model))
	}
	// The same response can exist in live and archived copies. Match Reader's
	// latest-call rule without building a second all-history map in the Go heap.
	// Rank only row identities; wide metadata strings do not need to travel
	// through the duplicate-selection window or its temporary sort buffers.
	if call != nil {
		rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.provider, c.model, c.session, c.project, f.rowid, c.at_ns,
			c.input_tokens, c.output_tokens, c.cache_read, c.cache_write, c.cache_write_1h FROM (
			SELECT c.rowid AS call_row,
			ROW_NUMBER() OVER (PARTITION BY c.id ORDER BY c.at_ns DESC, c.file_key) AS rank
			FROM calls c`+filter+`) selected
			JOIN calls c ON c.rowid=selected.call_row JOIN files f ON f.file_key=c.file_key
			WHERE selected.rank=1 ORDER BY c.at_ns, c.id`, args...)
		if err != nil {
			return err
		}
		var item Call
		var stamp, fileID int64
		for rows.Next() {
			if err := rows.Scan(&item.ID, &item.Provider, &item.Model, &item.Session, &item.Project, &fileID, &stamp,
				&item.Input, &item.Output, &item.CacheRead, &item.CacheWrite, &item.CacheWrite1h); err != nil {
				rows.Close()
				return err
			}
			item.Title = titles[fileID]
			item.At = time.Unix(0, stamp).UTC()
			call(item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if activity == nil {
		return nil
	}
	// Retain the fullest activity bucket independently for duration and prompts.
	rows, err := s.db.QueryContext(ctx, `SELECT c.provider, c.model, c.session, c.project, f.rowid, c.at_ns, selected.full_ms, selected.full_prompts FROM (
		SELECT c.rowid AS activity_row,
		MAX(c.milliseconds) OVER (PARTITION BY c.activity_key) AS full_ms,
		MAX(c.prompts) OVER (PARTITION BY c.activity_key) AS full_prompts,
		ROW_NUMBER() OVER (PARTITION BY c.activity_key ORDER BY c.at_ns DESC, c.file_key) AS rank
		FROM activity c`+filter+`) selected
		JOIN activity c ON c.rowid=selected.activity_row JOIN files f ON f.file_key=c.file_key
		WHERE selected.rank=1 ORDER BY c.at_ns, c.activity_key`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var activityItem Activity
	var activityStamp, fileID int64
	for rows.Next() {
		if err := rows.Scan(&activityItem.Provider, &activityItem.Model, &activityItem.Session, &activityItem.Project, &fileID, &activityStamp, &activityItem.Milliseconds, &activityItem.Prompts); err != nil {
			return err
		}
		activityItem.Title = titles[fileID]
		activityItem.At = time.Unix(0, activityStamp).UTC()
		activity(activityItem)
	}
	return rows.Err()
}
