package store

import (
	"context"
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// AppendEvent appends an evidence event to the append-only log, chaining the
// previous event's hash and recording the payload digest. It returns the
// assigned sequence number.
func (s *Store) AppendEvent(ctx context.Context, tx *sql.Tx, taskID string, e *task.EvidenceEvent, payload any) (int64, error) {
	payloadJSON, err := encode(payload)
	if err != nil {
		return 0, err
	}
	var prevHash string
	var seq int64
	err = tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM events WHERE task_id = ?`, taskID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if seq > 0 {
		err = tx.QueryRow(`SELECT hash FROM events WHERE task_id = ? AND seq = ?`, taskID, seq).Scan(&prevHash)
		if err != nil {
			return 0, err
		}
	}
	seq++
	e.Sequence = seq
	e.PrevHash = prevHash
	e.PayloadDigest = task.Digest([]byte(payloadJSON))
	e.Hash = task.Digest([]byte(payloadJSON + prevHash))
	if _, err := tx.Exec(`
		INSERT INTO events(task_id, logical_time, operation_id, generation, type, payload_digest, prev_hash, hash, payload)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, int64(e.LogicalTime), e.OperationID, int64(e.Generation), string(e.Type),
		e.PayloadDigest, e.PrevHash, e.Hash, payloadJSON); err != nil {
		return 0, err
	}
	return seq, nil
}

// Evidence returns the append-only events for a task ordered by sequence,
// paginated by the stable cursor (last sequence seen).
func (s *Store) Evidence(ctx context.Context, taskID string, after int64, limit int) ([]task.EvidenceEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT seq, logical_time, operation_id, generation, type, payload_digest, prev_hash, hash
		FROM events WHERE task_id = ? AND seq > ? ORDER BY seq LIMIT ?`, taskID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []task.EvidenceEvent
	for rows.Next() {
		var e task.EvidenceEvent
		if err := rows.Scan(&e.Sequence, &e.LogicalTime, &e.OperationID, &e.Generation, &e.Type, &e.PayloadDigest, &e.PrevHash, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// VerifyChain recomputes every event hash in order and reports whether the
// append-only log is intact. It is used by readiness and recovery checks.
func (s *Store) VerifyChain(ctx context.Context, taskID string) (bool, error) {
	rows, err := s.db.Query(`SELECT seq, payload, prev_hash, hash FROM events WHERE task_id = ? ORDER BY seq`, taskID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var prev string
	for rows.Next() {
		var seq int64
		var payload, prevHash, hash string
		if err := rows.Scan(&seq, &payload, &prevHash, &hash); err != nil {
			return false, err
		}
		if prevHash != prev {
			return false, nil
		}
		if hash != task.Digest([]byte(payload+prev)) {
			return false, nil
		}
		prev = hash
	}
	return true, rows.Err()
}

// EvidenceRoot returns the hash chain root for a task: the hash of the last
// event (which transitively chains every prior event), or empty for no events.
func (s *Store) EvidenceRoot(ctx context.Context, taskID string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT hash FROM events WHERE task_id = ? ORDER BY seq DESC LIMIT 1`, taskID).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}
