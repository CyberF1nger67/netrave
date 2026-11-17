// SPDX-License-Identifier: GPL-3.0-only

package database

import (
	"database/sql"
	"fmt"
	"netrave/internal/catalog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	_ "modernc.org/sqlite"

	"netrave/internal/folderstore"
	"netrave/internal/journal"
	"netrave/internal/labfile"
	"netrave/internal/permcatalog"
)

// DB - a wrapper over *sql.DB with helper methods.
// DB - a wrapper over *sql.DB. Journal - the event journal in a file next to the database.
type DB struct {
	*sql.DB
	Journal *journal.Journal
	// Folders - lab folders and placement (folders.yaml in the labs directory, see AttachFolders).
	Folders *folderstore.Store
	// Labs - lab projects (<labs_dir>/<id>/lab.yaml, see AttachLabs).
	Labs *labfile.Store
	// Catalog - images, machine templates and snapshots (files in the database directory, see Open).
	Catalog *catalog.Store
	// seq - the unique number of the opened database instance. In-memory caches use it as a key,
	// so data from a closed database does not go to a new one that took the same address.
	seq uint64
}

var dbSeq atomic.Uint64

// Seq returns the unique database instance number.
func (d *DB) Seq() uint64 { return d.seq }

// AttachLabs connects the labs directory.
func (d *DB) AttachLabs(dir string) error {
	ls, err := labfile.Open(dir)
	if err != nil {
		return err
	}
	d.Labs = ls
	return nil
}

// AttachFolders connects the lab folders file. Before the call, folders exist only in memory and are not persisted.
func (d *DB) AttachFolders(path string) error {
	fs, err := folderstore.Open(path)
	if err != nil {
		return err
	}
	d.Folders = fs
	return nil
}

// Open opens (and creates if necessary) the SQLite database. A new database gets the schema and initial data
// in one transaction. A database with a different schema version is not supported: it must be recreated.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(4)
	if err := sdb.Ping(); err != nil {
		sdb.Close()
		return nil, err
	}
	jrn, err := journal.Open(filepath.Join(filepath.Dir(path), "journal.jsonl"))
	if err != nil {
		sdb.Close()
		return nil, err
	}
	db := &DB{DB: sdb, Journal: jrn, Folders: folderstore.Memory(), seq: dbSeq.Add(1)}
	if err := db.AttachLabs(filepath.Join(filepath.Dir(path), "labs")); err != nil {
		sdb.Close()
		return nil, err
	}
	cat, err := catalog.Open(filepath.Join(filepath.Dir(path), "catalog"))
	if err != nil {
		sdb.Close()
		return nil, err
	}
	db.Catalog = cat
	if err := db.init(); err != nil {
		sdb.Close()
		return nil, err
	}
	return db, nil
}

// init creates the schema in an empty database or checks the version of an existing one.
func (d *DB) init() error {
	var ver int
	if err := d.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return err
	}
	switch {
	case ver == schemaVersion:
		return d.seedPermissions()
	case ver != 0:
		return fmt.Errorf("версия схемы базы %d не поддерживается (нужна %d): создайте базу заново", ver, schemaVersion)
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("создание схемы: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return d.seedPermissions()
}

// seedPermissions synchronizes the role permission catalog with the code.
// Rows are not deleted: if a permission disappears from the catalog, the API simply stops accepting it,
// and roles that already have it do not lose data. Names and groups are updated without losing assignments.
func (d *DB) seedPermissions() error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, p := range permcatalog.Catalog {
		if _, err := tx.Exec(`INSERT INTO permissions(key,title,group_key,group_title,sort)
			VALUES(?,?,?,?,?)
			ON CONFLICT(key) DO UPDATE SET title=excluded.title, group_key=excluded.group_key,
				group_title=excluded.group_title, sort=excluded.sort`,
			p.Key, p.Title, p.GroupKey, p.Group, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Audit writes a record to the event journal. userID == 0 means "no user".
func (d *DB) Audit(userID int64, username, action, detail, ip string) {
	d.AuditData(userID, username, action, detail, ip, nil)
}

// AuditData writes a journal record with additional fields (e.g. lab_id for lab wipes).
func (d *DB) AuditData(userID int64, username, action, detail, ip string, data map[string]any) {
	_ = d.Journal.Append(journal.Entry{UserID: userID, Username: username, Action: action, Detail: detail, IP: ip, Data: data})
}

// SweepSnapshots deletes snapshots of machines whose instance rows no longer exist. Previously this was done by a foreign key cascade;
// call it after deleting labs and users, before row numbers can be reused.
func (d *DB) SweepSnapshots() error {
	rows, err := d.Query(`SELECT id FROM instance_devices`)
	if err != nil {
		return err
	}
	defer rows.Close()
	live := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		live[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return d.Catalog.DropSnapshotsNotIn(live)
}

// NameTakenAtRoot reports that the name is taken by a folder or lab at the root. The URL /labs/<name> must
// unambiguously lead to one of them, so such names are not repeated. exceptLab is the lab being
// renamed: its old name is not considered taken.
func (d *DB) NameTakenAtRoot(name string, exceptLab int64) bool {
	want := strings.ToLower(strings.TrimSpace(name))
	if d.Folders == nil {
		return false
	}
	for _, f := range d.Folders.Folders() {
		if strings.ToLower(f.Name) == want {
			return true
		}
	}
	if d.Labs == nil {
		return false
	}
	placed := d.Folders.Placements()
	for _, l := range d.Labs.Labs() {
		if l.ID == exceptLab {
			continue
		}
		if folder, ok := placed[l.ID]; ok && folder != 0 {
			continue
		}
		if strings.ToLower(l.Name) == want {
			return true
		}
	}
	return false
}
