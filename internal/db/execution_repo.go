package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"auto-take-go/internal/models"
)

// Create inserts a new execution record.
func CreateExecution(exec *models.Execution) error {
	_, err := DB.Exec(
		`INSERT INTO executions (id, task_id, status, worker_id, start_time, end_time,
		 result_summary, result_log, screenshot_path, source_path, variables, retry_count, error_msg)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exec.ID, exec.TaskID, string(exec.Status), nullString(exec.WorkerID),
		exec.StartTime, exec.EndTime, exec.ResultSummary, exec.ResultLog,
		nullString(exec.ScreenshotPath), nullString(exec.SourcePath),
		exec.Variables, exec.RetryCount, nullString(exec.ErrorMsg),
	)
	return err
}

// GetExecutionByID retrieves an execution by ID.
func GetExecutionByID(id string) (*models.Execution, error) {
	var e models.Execution
	var workerID, screenshotPath, sourcePath, errorMsg sql.NullString

	err := DB.QueryRow(
		`SELECT id, task_id, status, worker_id, start_time, end_time,
		 result_summary, result_log, screenshot_path, source_path, variables, retry_count, error_msg
		 FROM executions WHERE id=?`,
		id,
	).Scan(
		&e.ID, &e.TaskID, &e.Status, &workerID,
		&e.StartTime, &e.EndTime, &e.ResultSummary, &e.ResultLog,
		&screenshotPath, &sourcePath, &e.Variables, &e.RetryCount, &errorMsg,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if workerID.Valid {
		e.WorkerID = workerID.String
	}
	if screenshotPath.Valid {
		e.ScreenshotPath = screenshotPath.String
	}
	if sourcePath.Valid {
		e.SourcePath = sourcePath.String
	}
	if errorMsg.Valid {
		e.ErrorMsg = errorMsg.String
	}
	return &e, nil
}

// ListExecutions returns filtered, paginated executions.
func ListExecutions(filter models.ExecutionFilter) ([]*models.Execution, int, error) {
	var args []interface{}
	var conditions []string

	if filter.TaskID != "" {
		conditions = append(conditions, "task_id = ?")
		args = append(args, filter.TaskID)
	}
	if filter.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.StartTimeFrom != nil {
		conditions = append(conditions, "start_time >= ?")
		args = append(args, *filter.StartTimeFrom)
	}
	if filter.StartTimeTo != nil {
		conditions = append(conditions, "start_time <= ?")
		args = append(args, *filter.StartTimeTo)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := DB.QueryRow(
		"SELECT COUNT(*) FROM executions "+where, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset

	query := fmt.Sprintf(
		`SELECT id, task_id, status, worker_id, start_time, end_time,
		 result_summary, result_log, screenshot_path, source_path, variables, retry_count, error_msg
		 FROM executions %s ORDER BY start_time DESC LIMIT ? OFFSET ?`,
		where,
	)
	args = append(args, limit, offset)

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var execs []*models.Execution
	for rows.Next() {
		var e models.Execution
		var workerID, screenshotPath, sourcePath, errorMsg sql.NullString
		if err := rows.Scan(
			&e.ID, &e.TaskID, &e.Status, &workerID,
			&e.StartTime, &e.EndTime, &e.ResultSummary, &e.ResultLog,
			&screenshotPath, &sourcePath, &e.Variables, &e.RetryCount, &errorMsg,
		); err != nil {
			return nil, 0, err
		}
		if workerID.Valid {
			e.WorkerID = workerID.String
		}
		if screenshotPath.Valid {
			e.ScreenshotPath = screenshotPath.String
		}
		if sourcePath.Valid {
			e.SourcePath = sourcePath.String
		}
		if errorMsg.Valid {
			e.ErrorMsg = errorMsg.String
		}
		execs = append(execs, &e)
	}
	return execs, total, rows.Err()
}

// UpdateExecution modifies an existing execution record.
func UpdateExecution(exec *models.Execution) error {
	_, err := DB.Exec(
		`UPDATE executions SET task_id=?, status=?, worker_id=?, start_time=?, end_time=?,
		 result_summary=?, result_log=?, screenshot_path=?, source_path=?,
		 variables=?, retry_count=?, error_msg=?
		 WHERE id=?`,
		exec.TaskID, string(exec.Status), nullString(exec.WorkerID),
		exec.StartTime, exec.EndTime, exec.ResultSummary, exec.ResultLog,
		nullString(exec.ScreenshotPath), nullString(exec.SourcePath),
		exec.Variables, exec.RetryCount, nullString(exec.ErrorMsg),
		exec.ID,
	)
	return err
}

// UpdateExecutionStatus updates only the status and worker_id of an execution.
func UpdateExecutionStatus(id, status, workerID string) error {
	_, err := DB.Exec(
		`UPDATE executions SET status=?, worker_id=? WHERE id=?`,
		status, nullString(workerID), id,
	)
	return err
}

// ClaimTask atomically claims a pending execution for a worker using row lock.
// Returns the claimed execution or nil if none available.
func ClaimTask(workerID string, concurrent int) (*models.Execution, error) {
	tx, err := DB.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	// Lock a pending execution whose task is enabled.
	var execID string
	err = tx.QueryRow(
		`UPDATE executions
		 SET status='running', worker_id=?, start_time=NOW()
		 WHERE id = (
		     SELECT id FROM (
		         SELECT e.id FROM executions e
		         INNER JOIN tasks t ON e.task_id = t.id
		         WHERE e.status='pending' AND t.enabled=1
		         ORDER BY e.start_time ASC
		         LIMIT 1
		     ) AS t2
		 )
		 AND status='pending'
		 RETURNING id`,
		workerID,
	).Scan(&execID)
	if err == sql.ErrNoRows {
		tx.Rollback()
		return nil, nil
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return GetExecutionByID(execID)
}

// Stop marks an execution as stopped.
func StopExecution(execID string) error {
	_, err := DB.Exec(
		`UPDATE executions SET status='stopped', end_time=NOW() WHERE id=? AND status IN ('pending','running')`,
		execID,
	)
	return err
}

// UpdateExecutionResult updates the final result fields of an execution.
func UpdateExecutionResult(id, status, resultSummary, resultLog, errorMsg string) error {
	endTimeSQL := "NOW()"
	_, err := DB.Exec(
		fmt.Sprintf(
			`UPDATE executions SET status=?, result_summary=?, result_log=?, error_msg=?, end_time=%s WHERE id=?`,
			endTimeSQL,
		),
		status, nullString(resultSummary), nullString(resultLog), nullString(errorMsg), id,
	)
	return err
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
