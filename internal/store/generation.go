package store

import (
	"database/sql"

	"example.com/steel-fireproofing-evidence-closure/internal/errs"
	"example.com/steel-fireproofing-evidence-closure/internal/task"
)

// IncrementGeneration advances a task to the next generation exactly once. A
// concurrent or stale request whose expected generation no longer matches the
// current one returns CodeGenerationConflict.
func IncrementGeneration(tx *sql.Tx, taskID string, from task.Generation) (task.Generation, error) {
	var cur int64
	if err := tx.QueryRow(`SELECT current_generation FROM tasks WHERE id = ?`, taskID).Scan(&cur); err != nil {
		if err == sql.ErrNoRows {
			return 0, errs.New(errs.CodeNotFound, "task not found")
		}
		return 0, err
	}
	if cur != int64(from) {
		return 0, errs.New(errs.CodeGenerationConflict, "generation already advanced")
	}
	next := cur + 1
	if _, err := tx.Exec(`UPDATE tasks SET current_generation = ? WHERE id = ?`, next, taskID); err != nil {
		return 0, err
	}
	return task.Generation(next), nil
}
