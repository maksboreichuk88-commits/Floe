// Package audit implements local SQLite-backed request and response logging
// to provide a complete audit trail without sending data to third parties.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/floe-dev/floe/internal/config"
	"github.com/floe-dev/floe/internal/provider"
	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// Record represents a single audited LLM interaction.
type Record struct {
	ID               int64         `json:"id"`
	RequestID        string        `json:"request_id"`
	Timestamp        time.Time     `json:"timestamp"`
	ProviderID       string        `json:"provider_id"`
	Model            string        `json:"model"`
	ProjectID        string        `json:"project_id"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	LatencyMs        int64         `json:"latency_ms"`
	RequestBody      string        `json:"request_body,omitempty"`
	ResponseBody     string        `json:"response_body,omitempty"`
	Error            string        `json:"error,omitempty"`
}

// Store manages the SQLite database for audit records.
type Store struct {
	db        *sql.DB
	cfg       config.AuditConfig
	insertStmt *sql.Stmt
}

// NewStore initializes the audit database.
func NewStore(cfg config.AuditConfig) (*Store, error) {
	if !cfg.Enabled {
		return &Store{cfg: cfg}, nil
	}

	import "os"
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return nil, fmt.Errorf("creating audit directory: %w", err)
	}

	db, err := sql.Open("sqlite", cfg.DBPath+"?_journal=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("opening audit database: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing audit schema: %w", err)
	}

	stmt, err := db.Prepare(`
		INSERT INTO requests (
			request_id, timestamp, provider_id, model, project_id,
			prompt_tokens, completion_tokens, latency_ms,
			request_body, response_body, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("preparing insert statement: %w", err)
	}

	store := &Store{
		db:         db,
		cfg:        cfg,
		insertStmt: stmt,
	}

	// Run background cleanup
	if cfg.RetentionDays > 0 {
		go store.cleanupRoutine()
	}

	return store, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		provider_id TEXT NOT NULL,
		model TEXT NOT NULL,
		project_id TEXT,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		latency_ms INTEGER,
		request_body TEXT,
		response_body TEXT,
		error TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp);
	CREATE INDEX IF NOT EXISTS idx_requests_project ON requests(project_id);
	CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests(provider_id);
	`
	_, err := db.Exec(schema)
	return err
}

// LogSuccess records a successful interaction.
func (s *Store) LogSuccess(req *provider.ChatRequest, resp *provider.ChatResponse) error {
	if !s.cfg.Enabled || s.db == nil {
		return nil
	}

	var reqBody, respBody string
	if s.cfg.LogBodies {
		reqBytes, _ := json.Marshal(req)
		reqBody = string(reqBytes)
		respBytes, _ := json.Marshal(resp)
		respBody = string(respBytes)
	}

	_, err := s.insertStmt.Exec(
		req.Metadata.RequestID,
		req.Metadata.StartTime,
		resp.Provider,
		req.Model,
		req.Metadata.ProjectID,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Latency.Milliseconds(),
		reqBody,
		respBody,
		"", // no error
	)
	return err
}

// LogError records a failed interaction.
func (s *Store) LogError(req *provider.ChatRequest, err error) error {
	if !s.cfg.Enabled || s.db == nil {
		return nil
	}

	var reqBody string
	if s.cfg.LogBodies {
		reqBytes, _ := json.Marshal(req)
		reqBody = string(reqBytes)
	}

	latency := time.Since(req.Metadata.StartTime).Milliseconds()

	_, insertErr := s.insertStmt.Exec(
		req.Metadata.RequestID,
		req.Metadata.StartTime,
		req.Metadata.ProviderID,
		req.Model,
		req.Metadata.ProjectID,
		0, 0, latency,
		reqBody,
		"",
		err.Error(),
	)
	return insertErr
}

// Recent returns the N most recent audit records.
func (s *Store) Recent(ctx context.Context, limit int) ([]Record, error) {
	if !s.cfg.Enabled || s.db == nil {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, request_id, timestamp, provider_id, model, project_id,
		       prompt_tokens, completion_tokens, latency_ms,
		       request_body, response_body, error
		FROM requests
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []Record
	for rows.Next() {
		var r Record
		var reqBody, respBody, errMsg sql.NullString
		var proj sql.NullString

		if err := rows.Scan(
			&r.ID, &r.RequestID, &r.Timestamp, &r.ProviderID, &r.Model, &proj,
			&r.PromptTokens, &r.CompletionTokens, &r.LatencyMs,
			&reqBody, &respBody, &errMsg,
		); err != nil {
			return nil, err
		}

		if proj.Valid {
			r.ProjectID = proj.String
		}
		if reqBody.Valid {
			r.RequestBody = reqBody.String
		}
		if respBody.Valid {
			r.ResponseBody = respBody.String
		}
		if errMsg.Valid {
			r.Error = errMsg.String
		}

		records = append(records, r)
	}
	return records, nil
}

// Close cleanly shuts down the database connection.
func (s *Store) Close() error {
	if s.db != nil {
		if s.insertStmt != nil {
			s.insertStmt.Close()
		}
		return s.db.Close()
	}
	return nil
}

func (s *Store) cleanupRoutine() {
	ticker := time.NewTicker(24 * time.Hour)
	for range ticker.C {
		cutoff := time.Now().AddDate(0, 0, -s.cfg.RetentionDays)
		s.db.Exec("DELETE FROM requests WHERE timestamp < ?", cutoff)
	}
}
