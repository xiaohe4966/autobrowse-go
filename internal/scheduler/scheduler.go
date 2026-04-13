package scheduler

import (
	"auto-take-go/internal/db"
	"auto-take-go/internal/executor"
	"auto-take-go/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler 调度器，管理所有定时任务的生命周期
type Scheduler struct {
	mu        sync.Mutex
	cron      *cron.Cron
	jobs      map[string]cron.EntryID // taskID -> cron.EntryID
	stopFuncs map[string]context.CancelFunc // executionID -> cancel
	stopMu    sync.Mutex
	executor  *executor.Executor
}

// New 创建新的调度器
func New(exec *executor.Executor) *Scheduler {
	return &Scheduler{
		cron:      cron.New(cron.WithSeconds()),
		jobs:      make(map[string]cron.EntryID),
		stopFuncs: make(map[string]context.CancelFunc),
		executor:  exec,
	}
}

// Start 启动调度器，从 DB 加载所有 enabled 任务
func (s *Scheduler) Start() error {
	// 启动 cron 引擎
	s.cron.Start()

	// 从数据库加载所有启用的任务
	tasks, err := db.GetEnabledTasks()
	if err != nil {
		return fmt.Errorf("failed to load enabled tasks: %w", err)
	}

	// 注册所有任务
	for _, task := range tasks {
		if err := s.AddTask(&task); err != nil {
			// 记录错误但继续加载其他任务
			fmt.Printf("Failed to add task %s: %v\n", task.ID, err)
		}
	}

	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	// 停止所有正在运行的任务
	s.stopMu.Lock()
	for execID, cancel := range s.stopFuncs {
		cancel()
		delete(s.stopFuncs, execID)
	}
	s.stopMu.Unlock()

	// 停止 cron 引擎
	ctx := s.cron.Stop()
	<-ctx.Done()
}

// AddTask 添加任务到调度器
func (s *Scheduler) AddTask(task *models.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 如果任务已存在，先移除
	if entryID, exists := s.jobs[task.ID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, task.ID)
	}

	// 如果任务未启用，直接返回
	if !task.Enabled {
		return nil
	}

	// 根据调度类型添加任务
	switch task.ScheduleType {
	case models.ScheduleCron:
		return s.addCronTask(task)
	case models.ScheduleInterval:
		return s.addIntervalTask(task)
	default:
		// 无调度类型，不添加到 cron
		return nil
	}
}

// addCronTask 添加 cron 类型的任务
func (s *Scheduler) addCronTask(task *models.Task) error {
	cfg := task.GetScheduleConfig()
	if cfg.CronExpr == "" {
		return fmt.Errorf("cron expression is empty")
	}

	entryID, err := s.cron.AddFunc(cfg.CronExpr, func() {
		s.triggerWithJitter(task)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}

	s.jobs[task.ID] = entryID
	return nil
}

// addIntervalTask 添加间隔类型的任务
func (s *Scheduler) addIntervalTask(task *models.Task) error {
	cfg := task.GetScheduleConfig()
	if cfg.IntervalMs <= 0 {
		return fmt.Errorf("invalid interval")
	}

	// 使用 cron 的 Every 功能
	duration := time.Duration(cfg.IntervalMs) * time.Millisecond
	entryID, err := s.cron.AddFunc(fmt.Sprintf("@every %s", duration), func() {
		s.triggerWithJitter(task)
	})
	if err != nil {
		return fmt.Errorf("failed to add interval task: %w", err)
	}

	s.jobs[task.ID] = entryID
	return nil
}

// triggerWithJitter 带随机抖动的触发
func (s *Scheduler) triggerWithJitter(task *models.Task) {
	cfg := task.GetScheduleConfig()
	jitterSec := cfg.GetJitterSec()

	// 计算随机抖动时间 [0, jitterSec]
	var jitter time.Duration
	if jitterSec > 0 {
		jitter = time.Duration(rand.Intn(jitterSec)) * time.Second
	}
	// 额外增加 0-5 秒的随机时间
	extraJitter := time.Duration(rand.Intn(6)) * time.Second

	time.AfterFunc(jitter+extraJitter, func() {
		s.createExecution(task)
	})
}

// randDuration 生成 0 到 max 秒的随机时间
func randDuration(max int) time.Duration {
	return time.Duration(rand.Intn(max+1)) * time.Second
}

// createExecution 创建执行实例
func (s *Scheduler) createExecution(task *models.Task) {
	// 解析任务定义
	var taskDef models.TaskDefinition
	if err := json.Unmarshal(task.Definition, &taskDef); err != nil {
		fmt.Printf("Failed to parse task definition for task %s: %v\n", task.ID, err)
		return
	}

	// 创建执行记录
	exec, err := db.CreateExecution(task.ID)
	if err != nil {
		fmt.Printf("Failed to create execution for task %s: %v\n", task.ID, err)
		return
	}

	// 创建带取消功能的 context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutSec)*time.Second)
	s.RegisterExecution(exec.ID, cancel)

	// 异步执行任务
	go func() {
		defer s.StopExecution(exec.ID)

		err := s.executor.Run(ctx, exec, &taskDef)
		if err != nil {
			fmt.Printf("Task %s execution %s failed: %v\n", task.ID, exec.ID, err)
		}
	}()
}

// RemoveTask 从调度器移除任务
func (s *Scheduler) RemoveTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.jobs[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.jobs, taskID)
	}

	return nil
}

// TriggerNow 手动触发任务，立即创建执行实例
func (s *Scheduler) TriggerNow(taskID string) error {
	// 获取任务信息
	task, err := db.GetTaskByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if task == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	// 解析任务定义
	var taskDef models.TaskDefinition
	if err := json.Unmarshal(task.Definition, &taskDef); err != nil {
		return fmt.Errorf("failed to parse task definition: %w", err)
	}

	// 创建执行记录
	exec, err := db.CreateExecution(taskID)
	if err != nil {
		return fmt.Errorf("failed to create execution: %w", err)
	}

	// 创建带取消功能的 context
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutSec)*time.Second)
	s.RegisterExecution(exec.ID, cancel)

	// 异步执行任务
	go func() {
		defer s.StopExecution(exec.ID)

		err := s.executor.Run(ctx, exec, &taskDef)
		if err != nil {
			fmt.Printf("Manual trigger task %s execution %s failed: %v\n", taskID, exec.ID, err)
		}
	}()

	return nil
}

// StopExecution 停止正在运行的任务
func (s *Scheduler) StopExecution(execID string) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	if cancel, ok := s.stopFuncs[execID]; ok {
		cancel()
		delete(s.stopFuncs, execID)
	}
}

// RegisterExecution 登记一个正在运行 execution 的 cancelFunc
func (s *Scheduler) RegisterExecution(execID string, cancel context.CancelFunc) {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.stopFuncs[execID] = cancel
}

// GetRunningExecutions 获取正在运行的执行实例列表
func (s *Scheduler) GetRunningExecutions() []string {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()

	execIDs := make([]string, 0, len(s.stopFuncs))
	for execID := range s.stopFuncs {
		execIDs = append(execIDs, execID)
	}
	return execIDs
}
