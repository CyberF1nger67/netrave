// SPDX-License-Identifier: GPL-3.0-only

package database

// schemaVersion - the schema version this build understands. Databases have no migrations: if the version does not match,
// the file must be recreated.
const schemaVersion = 5

// schema creates the full schema from scratch. Lab topology (labs, machines, networks, wires, lab permissions)
// lives in files (see package labfile), so instances reference it by numeric id without
// foreign keys: deletion of machines, networks and wires and deletion of labs is done by code. "No reference" is NULL, not 0.
const schema = `
-- Users and sessions -------------------------------------------------------------------------------

CREATE TABLE users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin','user')),
	disabled      INTEGER NOT NULL DEFAULT 0,
	created_at    INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE sessions (
	id         TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	csrf       TEXT NOT NULL,
	ip         TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_exp  ON sessions(expires_at);

-- Permissions: global sections are granted via roles; per-lab permissions live in the lab's file (lab.yaml). -----

CREATE TABLE permissions (
	key         TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	group_key   TEXT NOT NULL,
	group_title TEXT NOT NULL,
	sort        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE roles (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	name            TEXT NOT NULL UNIQUE COLLATE NOCASE,
	description     TEXT NOT NULL DEFAULT '',
	max_running_labs INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	updated_at      INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);

CREATE TABLE role_permissions (
	role_id    INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
	permission TEXT NOT NULL REFERENCES permissions(key) ON DELETE CASCADE,
	PRIMARY KEY (role_id, permission)
);
CREATE INDEX idx_role_permissions_perm ON role_permissions(permission);

CREATE TABLE user_roles (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role_id    INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	PRIMARY KEY (user_id, role_id)
);
CREATE INDEX idx_user_roles_role ON user_roles(role_id);






-- User instances ----------------------------------------------------------------------------
-- lab_instances — a user's instance of a lab. instance_* — its working state and settings,
-- which the user can change without affecting the shared project.

CREATE TABLE lab_instances (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	lab_id     INTEGER NOT NULL,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')),
	UNIQUE (lab_id, user_id)
);
CREATE INDEX idx_lab_instances_user ON lab_instances(user_id);

-- Instance machine: a copy of the device settings at instance creation time, and its launch history.
-- Mutable process state (state, pid, ports, error) lives only in memory in the QEMU manager.
CREATE TABLE instance_devices (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id     INTEGER NOT NULL REFERENCES lab_instances(id) ON DELETE CASCADE,
	device_id       INTEGER NOT NULL,
	image_id        INTEGER,
	layer_image_id  INTEGER,
	image_own       INTEGER NOT NULL DEFAULT 0,
	machine         TEXT NOT NULL DEFAULT 'pc',
	cpu             INTEGER NOT NULL DEFAULT 1,
	ram_mb          INTEGER NOT NULL DEFAULT 1024,
	nics            INTEGER NOT NULL DEFAULT 1,
	nic_model       TEXT NOT NULL DEFAULT 'virtio-net-pci',
	iface_type      TEXT NOT NULL DEFAULT 'ens',
	iface_start     INTEGER NOT NULL DEFAULT 0,
	disk_bus        TEXT NOT NULL DEFAULT 'virtio',
	bios            TEXT NOT NULL DEFAULT 'seabios' CHECK(bios IN ('seabios','uefi')),
	console         TEXT NOT NULL DEFAULT 'serial' CHECK(console IN ('serial','vnc')),
	extra_args      TEXT NOT NULL DEFAULT '',
	applied         TEXT NOT NULL DEFAULT '',
	started_at      INTEGER NOT NULL DEFAULT 0,
	UNIQUE (instance_id, device_id)
);
CREATE INDEX idx_instance_devices_image ON instance_devices(image_id);
CREATE INDEX idx_instance_devices_layer ON instance_devices(layer_image_id);

CREATE TABLE instance_links (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id   INTEGER NOT NULL REFERENCES lab_instances(id) ON DELETE CASCADE,
	connection_id INTEGER NOT NULL,
	up            INTEGER NOT NULL DEFAULT 1,
	UNIQUE (instance_id, connection_id)
);

CREATE TABLE instance_networks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	instance_id INTEGER NOT NULL REFERENCES lab_instances(id) ON DELETE CASCADE,
	network_id  INTEGER NOT NULL,
	subnet      TEXT NOT NULL DEFAULT '',
	gateway     TEXT NOT NULL DEFAULT '',
	dhcp_start  TEXT NOT NULL DEFAULT '',
	dhcp_end    TEXT NOT NULL DEFAULT '',
	UNIQUE (instance_id, network_id)
);

-- Service -----------------------------------------------------------------------------------------
-- Event and lab reset log is kept in the journal.jsonl file (journal package), not in the database.



`
