package models

import (
	"encoding/json"
	"time"
)

// Task 任务定义
type Task struct {
	ID           string          `json:"id" db:"id"`
	Name         string          `json:"name" db:"name"`
	Description  string          `json:"description" db:"description"`
	Enabled      bool            `json:"enabled" db:"enabled"`
	Definition   json.RawMessage `json:"definition" db:"definition"`
	ScheduleType string          `json:"schedule_type" db:"schedule_type"`
	ScheduleCfg  json.RawMessage `json:"schedule_cfg" db:"schedule_cfg"`
	TimeoutSec   int             `json:"timeout_sec" db:"timeout_sec"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

// GetScheduleConfig 解析调度配置
func (t *Task) GetScheduleConfig() *ScheduleConfig {
	var cfg ScheduleConfig
	if len(t.ScheduleCfg) > 0 {
		json.Unmarshal(t.ScheduleCfg, &cfg)
	}
	return &cfg
}

// ScheduleConfig 调度配置
type ScheduleConfig struct {
	CronExpr   string `json:"cronExpr,omitempty"`
	IntervalMs int64  `json:"intervalMs,omitempty"`
	JitterSec  int    `json:"jitterSec,omitempty"`
}

// GetJitterSec 获取 jitter 秒数，默认 60
func (c *ScheduleConfig) GetJitterSec() int {
	if c.JitterSec <= 0 {
		return 60
	}
	return c.JitterSec
}

// Execution 执行实例
type Execution struct {
	ID             string          `json:"id" db:"id"`
	TaskID         string          `json:"task_id" db:"task_id"`
	Status         string          `json:"status" db:"status"`
	WorkerID       string          `json:"worker_id" db:"worker_id"`
	StartTime      *time.Time      `json:"start_time" db:"start_time"`
	EndTime        *time.Time      `json:"end_time" db:"end_time"`
	ResultSummary  string          `json:"result_summary" db:"result_summary"`
	ResultLog      string          `json:"result_log" db:"result_log"`
	ScreenshotPath string          `json:"screenshot_path" db:"screenshot_path"`
	SourcePath     string          `json:"source_path" db:"source_path"`
	Variables      json.RawMessage `json:"variables" db:"variables"`
	RetryCount     int             `json:"retry_count" db:"retry_count"`
	ErrorMsg       string          `json:"error_msg" db:"error_msg"`
}

// Worker Worker 定义
type Worker struct {
	ID             string          `json:"id" db:"id"`
	Name           string          `json:"name" db:"name"`
	LastHeartbeat  *time.Time      `json:"last_heartbeat" db:"last_heartbeat"`
	Status         string          `json:"status" db:"status"`
	Concurrent     int             `json:"concurrent" db:"concurrent"`
	CurrentLoad    int             `json:"current_load" db:"current_load"`
	Tags           json.RawMessage `json:"tags" db:"tags"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// TaskTemplate 任务模板
type TaskTemplate struct {
	ID          string          `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description string          `json:"description" db:"description"`
	Definition  json.RawMessage `json:"definition" db:"definition"`
	Tags        json.RawMessage `json:"tags" db:"tags"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

// ExecutionStatus 执行状态常量
const (
	ExecutionPending = "pending"
	ExecutionRunning = "running"
	ExecutionSuccess = "success"
	ExecutionFailed  = "failed"
	ExecutionStopped = "stopped"
	ExecutionRetry   = "retry"
)

// WorkerStatus Worker 状态常量
const (
	WorkerIdle    = "idle"
	WorkerBusy    = "busy"
	WorkerOffline = "offline"
)

// ScheduleType 调度类型常量
const (
	ScheduleNone     = "none"
	ScheduleCron     = "cron"
	ScheduleInterval = "interval"
)
