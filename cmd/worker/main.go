package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"auto-take-go/internal/models"
)

// TaskDefinition 是 models.TaskDefinition 的别名
type TaskDefinition = models.TaskDefinition

func main() {
	server := flag.String("server", "http://localhost:8099", "Server URL")
	id := flag.String("id", "", "Worker ID (auto-generated UUID if empty)")
	name := flag.String("name", "", "Worker display name")
	concurrent := flag.Int("concurrent", 1, "Max concurrent tasks")
	tagsStr := flag.String("tags", "", "Worker tags (comma separated)")
	secret := flag.String("secret", "", "Worker auth secret (or set WORKER_SECRET env)")
	flag.Parse()

	// Resolve secret: CLI flag > env var
	if *secret == "" {
		*secret = os.Getenv("WORKER_SECRET")
	}

	// Generate worker ID if not provided
	if *id == "" {
		*id = uuid.New().String()
	}

	// Default name
	if *name == "" {
		*name = "worker-" + (*id)[:8]
	}

	// Parse tags
	var tags []string
	if *tagsStr != "" {
		for _, t := range strings.Split(*tagsStr, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	log.Printf("[worker] starting id=%s name=%s concurrent=%d tags=%v server=%s", *id, *name, *concurrent, tags, *server)

	// Create HTTP client
	client := NewClient(*server, *secret)

	// Register worker
	ctx := context.Background()
	regReq := &RegisterRequest{
		ID:         *id,
		Name:       *name,
		Concurrent: *concurrent,
		Tags:       tags,
	}
	if err := client.Register(ctx, regReq); err != nil {
		log.Fatalf("[worker] register failed: %v", err)
	}
	log.Printf("[worker] registered successfully")

	// Create runner
	runner, err := NewRunner(client)
	if err != nil {
		log.Fatalf("[worker] create runner failed: %v", err)
	}
	defer runner.Close()

	// Heartbeat goroutine
	currentLoad := 0
	go heartbeatLoop(client, *id, &currentLoad)

	// Task poll loop
	go pollLoop(client, runner, *id, *concurrent, &currentLoad)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("[worker] received signal %v, shutting down...", sig)
}

func heartbeatLoop(client *Client, workerID string, load *int) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Heartbeat(ctx, &HeartbeatRequest{
			ID:   workerID,
			Load: *load,
		})
		cancel()
		if err != nil {
			log.Printf("[worker] heartbeat failed: %v", err)
		}
	}
}

func pollLoop(client *Client, runner *Runner, workerID string, maxConcurrent int, load *int) {
	for {
		// Check current load
		if *load >= maxConcurrent {
			time.Sleep(3 * time.Second)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		task, err := client.PollTask(ctx, workerID)
		cancel()

		if err != nil {
			// No task available or error
			time.Sleep(5*time.Second + time.Duration(rand.Intn(2000))*time.Millisecond)
			continue
		}

		if task == nil {
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("[worker] received task execution_id=%s task_id=%s", task.ExecutionID, task.TaskID)

		// Increment load
		*load++

		// Run task in goroutine
		go func() {
			defer func() {
				*load--
			}()
			runner.Run(task.ExecutionID, task.Definition)
		}()
	}
}

// parseDefinition parses a JSON string into TaskDefinition.
func parseDefinition(jsonStr string) (*TaskDefinition, error) {
	var def TaskDefinition
	if err := json.Unmarshal([]byte(jsonStr), &def); err != nil {
		return nil, fmt.Errorf("parse definition: %w", err)
	}
	return &def, nil
}
