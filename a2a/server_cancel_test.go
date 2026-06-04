package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/costa92/llm-agent-comm/internal/testenv"
)

// startServerWithSkill stands up an httptest server with a single named
// skill backed by the given handler.
func startServerWithSkill(t *testing.T, name string, handler SkillHandler) *httptest.Server {
	t.Helper()
	if err := testenv.CanStartHTTPServer(); err != nil {
		t.Skipf("local HTTP server unavailable in this environment: %v", err)
	}
	s := NewServer("test", "test server")
	s.RegisterSkill(name, "", handler)
	return httptest.NewServer(s.HTTPHandler())
}

// postTask creates one task via POST /tasks and returns its ID.
func postTask(t *testing.T, srv *httptest.Server, skill, inputJSON string) string {
	t.Helper()
	body, _ := json.Marshal(struct {
		Skill string          `json:"skill"`
		Input json.RawMessage `json:"input"`
	}{Skill: skill, Input: json.RawMessage(inputJSON)})
	resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /tasks status=%d body=%s", resp.StatusCode, string(raw))
	}
	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode created task: %v", err)
	}
	return task.ID
}

// getTask fetches the task snapshot via GET /tasks/{id}.
func getTask(t *testing.T, srv *httptest.Server, id string) *Task {
	t.Helper()
	resp, err := http.Get(srv.URL + "/tasks/" + id)
	if err != nil {
		t.Fatalf("GET /tasks/%s: %v", id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /tasks/%s status=%d body=%s", id, resp.StatusCode, string(raw))
	}
	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return &task
}

// Test 1: real cancel — slow handler 5s, DELETE should make state move to
// TaskFailed within 500ms wallclock (proving the handler ctx was actually
// cancelled, not that the handler ran to completion).
func TestDeleteTask_CancelsRunningHandler(t *testing.T) {
	started := make(chan struct{})
	srv := startServerWithSkill(t, "slow", func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return json.RawMessage(`{"done":true}`), nil
		}
	})
	defer srv.Close()

	id := postTask(t, srv, "slow", `{"k":"v"}`)

	// Wait for worker goroutine to actually be inside handler.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("handler never started")
	}

	start := time.Now()
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/tasks/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /tasks/%s: %v", id, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status=%d, want 200 or 204", resp.StatusCode)
	}

	// Poll GET until state leaves Running/Pending.
	var finalState TaskState
	var finalErr string
	for i := 0; i < 50; i++ {
		time.Sleep(20 * time.Millisecond)
		task := getTask(t, srv, id)
		if task.State != TaskRunning && task.State != TaskPending {
			finalState = task.State
			finalErr = task.Error
			break
		}
	}

	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel took %v, want < 500ms (handler should not run to 5s)", elapsed)
	}
	if finalState != TaskFailed {
		t.Errorf("final state = %q, want TaskFailed", finalState)
	}
	if !strings.Contains(strings.ToLower(finalErr), "cancel") {
		t.Errorf("task.Error = %q, want substring 'cancel'", finalErr)
	}
}

// Test 2: DELETE on a non-existent task id returns 404.
func TestDeleteTask_NotFound(t *testing.T) {
	srv := startServerWithSkill(t, "noop", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/tasks/does-not-exist", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status=%d, want 404", resp.StatusCode)
	}
}

// Test 3: DELETE on an already-completed task is a no-op: the terminal state
// must NOT be downgraded to TaskFailed.
func TestDeleteTask_AlreadyCompleted_NoOp(t *testing.T) {
	srv := startServerWithSkill(t, "fast", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})
	defer srv.Close()

	id := postTask(t, srv, "fast", `{}`)

	// Wait for it to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task := getTask(t, srv, id)
		if task.State == TaskCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	preTask := getTask(t, srv, id)
	if preTask.State != TaskCompleted {
		t.Fatalf("task never completed; state=%q", preTask.State)
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/tasks/"+id, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE status=%d, want 200 or 204 for no-op", resp.StatusCode)
	}

	postTask := getTask(t, srv, id)
	if postTask.State != TaskCompleted {
		t.Errorf("state after DELETE = %q, want TaskCompleted (no downgrade)", postTask.State)
	}
}

// Test 4: GET /tasks/{id} continues to work after the DELETE handler is
// registered — the method dispatch must not break the existing GET path.
func TestDeleteTask_GetMethodOnSamePath_StillWorks(t *testing.T) {
	srv := startServerWithSkill(t, "fast", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"v":1}`), nil
	})
	defer srv.Close()

	id := postTask(t, srv, "fast", `{}`)

	// GET should succeed for either pending/running/completed.
	task := getTask(t, srv, id)
	if task.ID != id {
		t.Errorf("GET returned id=%q, want %q", task.ID, id)
	}
}

// Test 5: PUT /tasks/{id} (or other unknown methods) returns 405.
func TestDeleteTask_WrongMethod_Returns405(t *testing.T) {
	srv := startServerWithSkill(t, "fast", func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
	defer srv.Close()

	id := postTask(t, srv, "fast", `{}`)

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/tasks/"+id, strings.NewReader(""))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("PUT status=%d, want 405", resp.StatusCode)
	}
}
