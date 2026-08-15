package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/backlink-orchestrator/internal/auth"
	"github.com/backlink-orchestrator/internal/config"
	"github.com/backlink-orchestrator/internal/database"
)

func main() {
	fmt.Println("=== Starting Orchestrator Smoke Test ===")

	cfg := config.Load()
	db, err := database.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		panic("DB connection failed: " + err.Error())
	}
	defer db.Close()

	// Wait for server to be up
	time.Sleep(2 * time.Second)

	// Admin login
	fmt.Println("1. Logging in as Admin...")
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Post("http://localhost:8080/login", "application/x-www-form-urlencoded", bytes.NewBufferString("username=admin&password=admin"))
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		panic(fmt.Sprintf("Admin login failed! Code: %v", resp.StatusCode))
	}

	// Create Bootstrap Token in DB directly (simulation)
	fmt.Println("2. Generating Bootstrap Token...")
	hashedToken := auth.HashToken("smoke-token")
	_, err = db.Exec("INSERT INTO bootstrap_tokens (token_hash, expires_at, created_by, status) VALUES ($1, NOW() + INTERVAL '1 hour', 'smoke_test', 'ACTIVE') ON CONFLICT DO NOTHING", hashedToken)
	if err != nil {
		panic(err)
	}

	// Register Worker
	fmt.Println("3. Simulating Worker Registration...")
	reqBody := `{"worker_id":"smoke-worker","hostname":"test","os":"linux","architecture":"amd64","cpu_count":2,"memory_mb":1024,"version":"1.0"}`
	req, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/workers/register", bytes.NewBufferString(reqBody))
	req.Header.Set("Authorization", "Bearer smoke-token")

	respReg, err := http.DefaultClient.Do(req)
	if err != nil || respReg.StatusCode != http.StatusOK {
		panic("Worker registration failed")
	}

	var regRes struct {
		Token string `json:"token"`
	}
	json.NewDecoder(respReg.Body).Decode(&regRes)
	tokenRaw := regRes.Token

	// Create Job
	fmt.Println("4. Submitting a new Job...")
	db.Exec("INSERT INTO jobs (id, job_id, dataset, crawl_id, status, total_tasks, queued_tasks) VALUES (gen_random_uuid(), 'SMOKE-JOB', 'SMOKE', '1', 'QUEUED', 1, 1)")
	db.Exec("INSERT INTO tasks (id, task_id, job_id, dataset, source_path, status) VALUES (gen_random_uuid(), 'SMOKE-TASK-1', (SELECT id FROM jobs WHERE job_id = 'SMOKE-JOB'), 'SMOKE', 'path1', 'QUEUED')")

	// Heartbeat
	fmt.Println("5. Sending Heartbeat...")
	reqHbBody := `{"worker_id":"smoke-worker","status":"READY","cpu_percent":10.5,"memory_percent":50.0}`
	reqHb, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/workers/heartbeat", bytes.NewBufferString(reqHbBody))
	reqHb.Header.Set("Authorization", "Bearer "+tokenRaw)

	respHb, err := http.DefaultClient.Do(reqHb)
	if err != nil || respHb.StatusCode != http.StatusOK {
		panic("Heartbeat failed")
	}

	// Claim Task
	fmt.Println("6. Claiming Task...")
	reqClaimBody := `{"worker_id":"smoke-worker","capacity":{"cpu_count":2,"memory_mb":1024}}`
	reqClaim, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/tasks/claim", bytes.NewBufferString(reqClaimBody))
	reqClaim.Header.Set("Authorization", "Bearer "+tokenRaw)

	respClaim, err := http.DefaultClient.Do(reqClaim)
	if err != nil || respClaim.StatusCode != http.StatusOK {
		panic("Claim task failed")
	}
	var claimRes struct {
		TaskID string `json:"task_id"`
	}
	json.NewDecoder(respClaim.Body).Decode(&claimRes)
	if claimRes.TaskID == "" {
		panic("Did not receive a task ID")
	}

	// Complete Task
	fmt.Println("7. Completing Task...")
	reqCompBody := `{"attempt_id":"1","processed_bytes":100,"records_processed":10,"links_found":5,"output_uri":"r2://smoke"}`
	reqComp, _ := http.NewRequest("POST", "http://localhost:8080/api/v1/tasks/"+claimRes.TaskID+"/complete", bytes.NewBufferString(reqCompBody))
	reqComp.Header.Set("Authorization", "Bearer "+tokenRaw)

	respComp, err := http.DefaultClient.Do(reqComp)
	if err != nil || respComp.StatusCode != http.StatusOK {
		panic("Complete task failed")
	}

	fmt.Println("=== Smoke Test Passed! ===")
}
