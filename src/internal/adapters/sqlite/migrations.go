package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Go steps run after a version's SQL in the same transaction, for data
// rewrites SQL alone cannot express.
var migrationSteps = map[int]func(*sql.Tx) error{14: archiveLegacySnapshots}

// A file holding only one ADD COLUMN is skipped when the column exists:
// older builds added some columns before recording their version.
var addColumn = regexp.MustCompile(`(?is)^\s*ALTER TABLE (\w+) ADD COLUMN (\w+)[^;]*;\s*$`)

// The baseline is idempotent and reapplied on every open, as before the
// runner existed, so databases that recorded it without every table recover.
const baselineVersion = 6

type migration struct {
	version    int
	name, body string
}

// loadMigrations reads migrations/NNN_name.sql in version order.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	out := make([]migration, 0, len(entries))
	seen := map[int]bool{}
	for _, entry := range entries {
		number, name, ok := strings.Cut(strings.TrimSuffix(entry.Name(), ".sql"), "_")
		version, err := strconv.Atoi(number)
		if !ok || err != nil || version <= 0 || seen[version] {
			return nil, fmt.Errorf("invalid migration file %q", entry.Name())
		}
		body, err := migrationFiles.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return nil, err
		}
		seen[version] = true
		out = append(out, migration{version, name, string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate applies, in order, every version schema_version has not recorded.
// Each version commits with its record, so an interrupted upgrade resumes.
func migrate(db *sql.DB) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("initialize schema version: %w", err)
	}
	for _, m := range migrations {
		if err := apply(db, m); err != nil {
			return fmt.Errorf("migrate %03d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func apply(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var recorded int
	if err := tx.QueryRow(`SELECT count(*) FROM schema_version WHERE version=?`, m.version).Scan(&recorded); err != nil {
		return err
	}
	if recorded > 0 {
		if m.version == baselineVersion {
			if _, err := tx.Exec(m.body); err != nil {
				return err
			}
			return tx.Commit()
		}
		return nil
	}
	present := 0
	if match := addColumn.FindStringSubmatch(m.body); match != nil {
		if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, match[1], match[2]).Scan(&present); err != nil {
			return err
		}
	}
	if present == 0 {
		if _, err := tx.Exec(m.body); err != nil {
			return err
		}
	}
	if step := migrationSteps[m.version]; step != nil {
		if err := step(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES(?)`, m.version); err != nil {
		return err
	}
	return tx.Commit()
}
