package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"auto-take-go/internal/models"
)

// Create inserts a new task.
func Create(task *models.Task) error {
	cfg := task.ScheduleCfg
	if cfg == nil {
		cfg = json.RawMessage("null")
	}
	_, err := DB.Exec(
		`INSERT INTO tasks (id, name, description, enabled, definition, schedule_type, schedule_cfg, timeout_sec, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`,
		task.ID, task.Name, task.Description, boolToTinyInt(task.Enabled),
		task.Definition, string(task.ScheduleType), cfg, task.TimeoutSec,
	)
	return err
}

// GetByID retrieves a task by its primary key.
func GetByID(id string) (*models.Task, error) {
	var t models.Task
	var enabled int
	var scheduleCfg sql.NullString

	err := DB.QueryRow(
		`SELECT id, name, description, enabled, definition, schedule_type, schedule_cfg, timeout_sec, created_at, updated_at
		 FROM tasks WHERE id = ?`,
		id,
	).Scan(
		&t.ID, &t.Name, &t.Description, &enabled,
		&t.Definition, &t.ScheduleType, &scheduleCfg,
		&t.TimeoutSec, &t.CreatedAt, &t.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Enabled = tinyIntToBool(enabled)
	if scheduleCfg.Valid {
		t.ScheduleCfg = json.RawMessage(scheduleCfg.String)
	}
	return &t, nil
}

// List returns paginated tasks with optional search and enabled filter.
func List(page, pageSize int, search string, enabled *bool) ([]*models.Task, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var args []interface{}
	var conditions []string

	if search != "" {
		conditions = append(conditions, "(name LIKE ? OR description LIKE ?)")
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	if enabled != nil {
		conditions = append(conditions, "enabled = ?")
		args = append(args, boolToTinyInt(*enabled))
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countSQL := "SELECT COUNT(*) FROM tasks " + where
	if err := DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := fmt.Sprintf(
		`SELECT id, name, description, enabled, definition, schedule_type, schedule_cfg, timeout_sec, created_at, updated_at
		 FROM tasks %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		where,
	)
	args = append(args, pageSize, offset)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tasks []*models.Task
	for rows.Next() {
		var t models.Task
		var enabled int
		var scheduleCfg sql.NullString
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &enabled,
			&t.Definition, &t.ScheduleType, &scheduleCfg,
			&t.TimeoutSec, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		t.Enabled = tinyIntToBool(enabled)
		if scheduleCfg.Valid {
			t.ScheduleCfg = json.RawMessage(scheduleCfg.String)
		}
		tasks = append(tasks, &t)
	}
	return tasks, total, rows.Err()
}

// Update modifies an existing task.
func Update(task *models.Task) error {
	cfg := task.ScheduleCfg
	if cfg == nil {
		cfg = json.RawMessage("null")
	}
	_, err := DB.Exec(
		`UPDATE tasks SET name=?, description=?, enabled=?, definition=?,
		 schedule_type=?, schedule_cfg=?, timeout_sec=?, updated_at=NOW()
		 WHERE id=?`,
		task.Name, task.Description, boolToTinyInt(task.Enabled),
		task.Definition, string(task.ScheduleType), cfg, task.TimeoutSec,
		task.ID,
	)
	return err
}

// Delete removes a task by ID (cascades to executions).
func Delete(id string) error {
	_, err := DB.Exec("DELETE FROM tasks WHERE id=?", id)
	return err
}

// GetSchedulable returns tasks that are enabled with a non-none schedule type.
func GetSchedulable() ([]*models.Task, error) {
	rows, err := DB.Query(
		`SELECT id, name, description, enabled, definition, schedule_type, schedule_cfg, timeout_sec, created_at, updated_at
		 FROM tasks WHERE enabled=1 AND schedule_type!='none'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*models.Task
	for rows.Next() {
		var t models.Task
		var enabled int
		var scheduleCfg sql.NullString
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &enabled,
			&t.Definition, &t.ScheduleType, &scheduleCfg,
			&t.TimeoutSec, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		t.Enabled = tinyIntToBool(enabled)
		if scheduleCfg.Valid {
			t.ScheduleCfg = json.RawMessage(scheduleCfg.String)
		}
		tasks = append(tasks, &t)
	}
	return tasks, rows.Err()
}

func boolToTinyInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func tinyIntToBool(i int) bool {
	return i != 0
}
