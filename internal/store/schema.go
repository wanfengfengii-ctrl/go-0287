package store

// schema is the ordered list of idempotent migration statements applied on
// startup. The migration version is tracked in the meta table so the store can
// report readiness and upgrade deterministically.
var schema = []string{
	`CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		floor TEXT NOT NULL,
		compartment TEXT NOT NULL,
		scope_json TEXT NOT NULL,
		plan_json TEXT NOT NULL,
		current_generation INTEGER NOT NULL,
		aggregate_version INTEGER NOT NULL,
		logical_clock INTEGER NOT NULL,
		terminal_state TEXT NOT NULL DEFAULT '',
		terminal_decided_by TEXT NOT NULL DEFAULT '',
		terminal_generation INTEGER NOT NULL DEFAULT 0,
		evidence_root TEXT NOT NULL DEFAULT '',
		credential_id TEXT NOT NULL DEFAULT '',
		locked_at INTEGER NOT NULL DEFAULT 0
	);`,
	`CREATE TABLE IF NOT EXISTS events (
		seq INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL,
		logical_time INTEGER NOT NULL,
		operation_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		type TEXT NOT NULL,
		payload_digest TEXT NOT NULL,
		prev_hash TEXT NOT NULL,
		hash TEXT NOT NULL,
		payload TEXT NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_events_task ON events(task_id, seq);`,
	`CREATE TABLE IF NOT EXISTS coats (
		task_id TEXT NOT NULL,
		face_id TEXT NOT NULL,
		coat_index INTEGER NOT NULL,
		generation INTEGER NOT NULL,
		spray_zone_id TEXT NOT NULL,
		material_batch_id TEXT NOT NULL,
		area_mm2 INTEGER NOT NULL,
		issued_grams INTEGER NOT NULL,
		recovered_grams INTEGER NOT NULL,
		approved_waste INTEGER NOT NULL,
		logical_time INTEGER NOT NULL,
		PRIMARY KEY (task_id, face_id, coat_index, generation)
	);`,
	`CREATE TABLE IF NOT EXISTS member_state (
		task_id TEXT NOT NULL,
		member_id TEXT NOT NULL,
		surface_prepared INTEGER NOT NULL DEFAULT 0,
		primer_applied INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (task_id, member_id)
	);`,
	`CREATE TABLE IF NOT EXISTS material_batches (
		task_id TEXT NOT NULL,
		batch_id TEXT NOT NULL,
		proof_summary TEXT NOT NULL,
		expired INTEGER NOT NULL,
		opened_grams INTEGER NOT NULL,
		topped_up_grams INTEGER NOT NULL,
		issued_grams INTEGER NOT NULL,
		recovered_grams INTEGER NOT NULL,
		approved_waste INTEGER NOT NULL,
		PRIMARY KEY (task_id, batch_id)
	);`,
	`CREATE TABLE IF NOT EXISTS material_ledger (
		task_id TEXT NOT NULL,
		batch_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		operation_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		grams INTEGER NOT NULL,
		logical_time INTEGER NOT NULL,
		PRIMARY KEY (task_id, batch_id, seq)
	);`,
	`CREATE TABLE IF NOT EXISTS leases (
		task_id TEXT NOT NULL,
		equipment_id TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		expires_at INTEGER NOT NULL,
		PRIMARY KEY (task_id, equipment_id)
	);`,
	`CREATE TABLE IF NOT EXISTS invocations (
		task_id TEXT NOT NULL,
		id TEXT NOT NULL,
		operation_id TEXT NOT NULL,
		equipment_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		logical_time INTEGER NOT NULL,
		fault TEXT NOT NULL DEFAULT '',
		attempt INTEGER NOT NULL,
		next_retry_at INTEGER NOT NULL,
		status TEXT NOT NULL,
		reading_ref TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (task_id, id)
	);`,
	`CREATE TABLE IF NOT EXISTS dry_film (
		task_id TEXT NOT NULL,
		face_id TEXT NOT NULL,
		point_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		value_um INTEGER NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_dry_film ON dry_film(task_id, face_id, point_id, generation);`,
	`CREATE TABLE IF NOT EXISTS bond (
		task_id TEXT NOT NULL,
		specimen_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		value_kpa INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS curing (
		task_id TEXT NOT NULL,
		face_id TEXT NOT NULL,
		coat_index INTEGER NOT NULL,
		temp_c INTEGER NOT NULL,
		humidity_rh INTEGER NOT NULL,
		logical_time INTEGER NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS reviews (
		task_id TEXT NOT NULL,
		reviewer_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		approved INTEGER NOT NULL,
		classes_json TEXT NOT NULL,
		PRIMARY KEY (task_id, reviewer_id, generation)
	);`,
	`CREATE TABLE IF NOT EXISTS defects (
		task_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		kind TEXT NOT NULL,
		point_id TEXT NOT NULL,
		face_id TEXT NOT NULL,
		member_id TEXT NOT NULL,
		material_batch_id TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS recoat_scopes (
		task_id TEXT NOT NULL,
		generation INTEGER NOT NULL,
		affected_json TEXT NOT NULL,
		coat_indices_json TEXT NOT NULL,
		PRIMARY KEY (task_id, generation)
	);`,
	`CREATE TABLE IF NOT EXISTS idempotency (
		caller TEXT NOT NULL,
		operation_id TEXT NOT NULL,
		request_digest TEXT NOT NULL,
		status TEXT NOT NULL,
		response_json TEXT NOT NULL,
		PRIMARY KEY (caller, operation_id)
	);`,
}
