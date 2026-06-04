package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SkillHandler is the function executed for one skill invocation.
type SkillHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// Server exposes skills + an async task lifecycle over HTTP.
//
// Routes:
//
//   GET    /skills      — JSON array of SkillDescriptor
//   POST   /tasks       — create a task; body = {skill, input}; returns Task
//   GET    /tasks/{id}  — fetch one task by ID
//   DELETE /tasks/{id}  — cancel a running task (no-op if already terminal)
type Server struct {
	name        string
	description string

	skillsMu sync.RWMutex
	skills   map[string]SkillHandler
	skillDoc map[string]string // name → description

	tasks  *taskStore
	idSeq  atomic.Uint64
}

// NewServer constructs an A2A server.
func NewServer(name, description string) *Server {
	return &Server{
		name:        name,
		description: description,
		skills:      make(map[string]SkillHandler),
		skillDoc:    make(map[string]string),
		tasks:       newTaskStore(),
	}
}

// Name returns the server's identity.
func (s *Server) Name() string { return s.name }

// RegisterSkill adds one skill.
func (s *Server) RegisterSkill(name, description string, handler SkillHandler) {
	s.skillsMu.Lock()
	defer s.skillsMu.Unlock()
	s.skills[name] = handler
	s.skillDoc[name] = description
}

// HTTPHandler returns the http.Handler exposing the routes above.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/skills", s.listSkills)
	mux.HandleFunc("/tasks", s.createTask)
	mux.HandleFunc("/tasks/", s.handleTask)
	return mux
}

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.skillsMu.RLock()
	out := make([]SkillDescriptor, 0, len(s.skills))
	for name, desc := range s.skillDoc {
		out = append(out, SkillDescriptor{Name: name, Description: desc})
	}
	s.skillsMu.RUnlock()
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Skill string          `json:"skill"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.skillsMu.RLock()
	handler, ok := s.skills[body.Skill]
	s.skillsMu.RUnlock()
	if !ok {
		http.Error(w, ErrSkillNotFound.Error()+": "+body.Skill, http.StatusNotFound)
		return
	}

	now := time.Now().UTC()
	taskID := fmt.Sprintf("task_%d", s.idSeq.Add(1))

	// Wire the cancel funcval on the Task BEFORE put + before spawning the
	// worker. This closes the worker-startup race window: if DELETE
	// /tasks/{id} arrives between put and the worker actually starting,
	// taskStore.cancelAndFail (lock-coupled) will still observe a non-nil
	// cancel and trigger ctx.Done() the moment the worker calls handler.
	ctx, cancel := context.WithCancel(context.Background())
	task := &Task{
		ID:        taskID,
		Skill:     body.Skill,
		Input:     body.Input,
		State:     TaskPending,
		CreatedAt: now,
		UpdatedAt: now,
		cancel:    cancel,
	}
	s.tasks.put(task)

	// Snapshot the initial-state Task before spawning the worker. writeJSON
	// encodes via reflection without holding taskStore.mu, so it must operate
	// on a value copy rather than the live pointer that runTask mutates.
	taskSnapshot := *task

	// Run asynchronously — caller polls /tasks/{id}.
	go s.runTask(ctx, taskID, handler, body.Input)

	writeJSON(w, http.StatusCreated, &taskSnapshot)
}

func (s *Server) runTask(ctx context.Context, id string, handler SkillHandler, input json.RawMessage) {
	// Always release the cancel funcval on exit. Safe to call after
	// cancelAndFail already invoked it — CancelFunc is idempotent.
	defer func() {
		s.tasks.update(id, func(t *Task) {
			if t.cancel != nil {
				t.cancel()
				t.cancel = nil
			}
		})
	}()
	s.tasks.update(id, func(t *Task) { t.State = TaskRunning })
	out, err := handler(ctx, input)
	s.tasks.update(id, func(t *Task) {
		// Honor the cancel — if DELETE already moved the task to
		// TaskFailed, do NOT overwrite it (handler may have ignored
		// ctx.Done() and completed anyway; the cancel still won).
		if t.State == TaskFailed {
			return
		}
		if err != nil {
			t.State = TaskFailed
			t.Error = err.Error()
			return
		}
		t.State = TaskCompleted
		t.Artifact = out
	})
}

// handleTask dispatches /tasks/{id} by method: GET fetches, DELETE cancels.
func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if id == "" || strings.Contains(id, "/") {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t, ok := s.tasks.get(id)
		if !ok {
			http.Error(w, ErrTaskNotFound.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, t)
	case http.MethodDelete:
		if !s.tasks.cancelAndFail(id, "canceled by DELETE") {
			http.Error(w, ErrTaskNotFound.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
