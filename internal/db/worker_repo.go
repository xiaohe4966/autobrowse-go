package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"auto-take-go/internal/models"
)

// Register inserts or updates a worker record.
func RegisterWorker(w *models.Worker) error {
	_, err := DB.Exec(
		`INSERT INTO workers (id, name, last_heartbeat, status, concurrent, current_load, tags, created_at)
		 VALUES (?, ?, NOW(), ?, ?, ?, ?, NOW())
		 ON DUPLICATE KEY UPDATE
		 name=VALUES(name), last_heartbeat=NOW(), status=VALUES(status),
		 concurrent=VALUES(concurrent), current_load=VALUES(current_load), tags=VALUES(tags)`,
		w.ID, nullString(w.Name), string(w.Status), w.Concurrent, w.CurrentLoad,
		w.Tags,
	)
	return err
}

// GetWorkerByID retrieves a worker by ID.
func GetWorkerByID(id string) (*models.Worker, error) {
	var w models.Worker
	var name, tags sql.NullString
	var lastHeartbeat sql.NullTime

	err := DB.QueryRow(
		`SELECT id, name, last_heartbeat, status, concurrent, current_load, tags, created_at
		 FROM workers WHERE id=?`,
		id,
	).Scan(
		&w.ID, &name, &lastHeartbeat, &w.Status,
		&w.Concurrent, &w.CurrentLoad, &tags, &w.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if name.Valid {
		w.Name = name.String
	}
	if lastHeartbeat.Valid {
		w.LastHeartbeat = &lastHeartbeat.Time
	}
	if tags.Valid {
		w.Tags = json.RawMessage(tags.String)
	}
	return &w, nil
}

// ListWorkers returns all registered workers.
func ListWorkers() ([]*models.Worker, error) {
	rows, err := DB.Query(
		`SELECT id, name, last_heartbeat, status, concurrent, current_load, tags, created_at
		 FROM workers ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workers []*models.Worker
	for rows.Next() {
		var w models.Worker
		var name, tags sql.NullString
		var lastHeartbeat sql.NullTime
		if err := rows.Scan(
			&w.ID, &name, &lastHeartbeat, &w.Status,
			&w.Concurrent, &w.CurrentLoad, &tags, &w.CreatedAt,
		); err != nil {
			return nil, err
		}
		if name.Valid {
			w.Name = name.String
		}
		if lastHeartbeat.Valid {
			w.LastHeartbeat = &lastHeartbeat.Time
		}
		if tags.Valid {
			w.Tags = json.RawMessage(tags.String)
		}
		workers = append(workers, &w)
	}
	return workers, rows.Err()
}

// UpdateHeartbeat refreshes the last_heartbeat timestamp for a worker.
func UpdateHeartbeat(id string) error {
	_, err := DB.Exec(
		"UPDATE workers SET last_heartbeat=NOW() WHERE id=?",
		id,
	)
	return err
}

// UpdateWorkerStatus changes a worker's status field.
func UpdateWorkerStatus(id, status string) error {
	_, err := DB.Exec(
		"UPDATE workers SET status=? WHERE id=?",
		status, id,
	)
	return err
}

// UpdateWorkerLoad sets the current_load for a worker.
func UpdateWorkerLoad(id string, load int) error {
	_, err := DB.Exec(
		"UPDATE workers SET current_load=? WHERE id=?",
		load, id,
	)
	return err
}

// SetOfflineIfExpired marks workers as offline if their heartbeat is older than the given interval.
func SetOfflineIfExpired(interval time.Duration) error {
	_, err := DB.Exec(
		`UPDATE workers SET status='offline'
		 WHERE last_heartbeat < DATE_SUB(NOW(), INTERVAL ? SECOND)
		 AND status != 'offline'`,
		int(interval.Seconds()),
	)
	return err
}
