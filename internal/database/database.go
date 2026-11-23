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

	"netrave/internal/journal"
	"netrave/internal/labfile"
	"netrave/internal/msgs"
	"netrave/internal/permcatalog"
)

// DB - a wrapper over *sql.DB with helper methods.
// DB - a wrapper over *sql.DB. Journal - the event journal in a file next to the database.
type DB struct {
	*sql.DB
	Journal *journal.Journal
	// Labs - lab projects (files in the laboratories directory, see AttachLabs).
	Labs *labfile.Store
	// Catalog - images and machine templates (YAML files in the data directory, see Open).
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
	db := &DB{DB: sdb, Journal: jrn, seq: dbSeq.Add(1)}
	// A database with another schema version is rejected before any laboratory or catalog files are touched.
	if err := db.init(); err != nil {
		sdb.Close()
		return nil, err
	}
	cat, err := catalog.Open(filepath.Dir(path))
	if err != nil {
		sdb.Close()
		return nil, err
	}
	db.Catalog = cat
	if err := db.AttachLabs(filepath.Join(filepath.Dir(path), "labs")); err != nil {
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
		return msgs.New("Database schema version {version} is not supported (version {required} is needed): recreate the database", map[string]string{"version": fmt.Sprint(ver), "required": fmt.Sprint(schemaVersion)})
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return msgs.Wrap(err, "Schema creation: {detail}", map[string]string{"detail": err.Error()})
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

// AuditKey writes a journal record whose text is a key with {name} parameters. The journal file keeps the
// English text, while the key and the parameters go into Data (msg_key, msg_params): the interface translates
// them when it shows the record. extra holds further Data fields (identifiers, counters).
func (d *DB) AuditKey(userID int64, username, action, key string, params map[string]string, extra map[string]any, ip string) {
	data := map[string]any{"msg_key": key, "msg_params": params}
	for k, v := range extra {
		data[k] = v
	}
	d.AuditData(userID, username, action, msgs.New(key, params).Error(), ip, data)
}

// AuditData writes a journal record with additional fields (e.g. lab_id for lab wipes).
func (d *DB) AuditData(userID int64, username, action, detail, ip string, data map[string]any) {
	_ = d.Journal.Append(journal.Entry{UserID: userID, Username: username, Action: action, Detail: detail, IP: ip, Data: data})
}

// NameTakenAtRoot reports that the name is taken by a folder or a lab at the root. The URL
// /labs/<name> must unambiguously lead to one of them, so such names are not repeated at the root.
// exceptLab is the lab being renamed: its old name is not considered taken.
func (d *DB) NameTakenAtRoot(name, exceptLab string) bool {
	if d.Labs == nil {
		return false
	}
	want := strings.ToLower(strings.TrimSpace(name))
	tree, err := d.Labs.Tree()
	if err != nil {
		return false
	}
	for _, f := range tree.Folders {
		if f.Path == "" && strings.ToLower(f.Name) == want {
			return true
		}
	}
	for _, l := range tree.Labs {
		if l.Folder == "" && !strings.EqualFold(l.Name, exceptLab) && strings.ToLower(l.Name) == want {
			return true
		}
	}
	return false
}
