package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ClientOptions configures Client.
type ClientOptions struct {
	HTTPClient   *http.Client  // nil → http.DefaultClient with Timeout
	Timeout      time.Duration // per-call HTTP timeout; default 30s
	PollInterval time.Duration // task polling interval; default 200ms
	PollMax      time.Duration // max time to poll before giving up; default 60s
}

// Client talks to an A2A server's HTTP routes.
type Client struct {
	endpoint string // e.g. http://localhost:6000
	http     *http.Client
	pollEvery time.Duration
	pollMax   time.Duration
}

// NewClient constructs a Client.
func NewClient(endpoint string, opts ClientOptions) *Client {
	if opts.HTTPClient == nil {
		t := opts.Timeout
		if t == 0 {
			t = 30 * time.Second
		}
		opts.HTTPClient = &http.Client{Timeout: t}
	}
	if opts.PollInterval == 0 {
		opts.PollInterval = 200 * time.Millisecond
	}
	if opts.PollMax == 0 {
		opts.PollMax = 60 * time.Second
	}
	return &Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		http:      opts.HTTPClient,
		pollEvery: opts.PollInterval,
		pollMax:   opts.PollMax,
	}
}

// ListSkills fetches the server's skill catalog.
func (c *Client) ListSkills(ctx context.Context) ([]SkillDescriptor, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/skills", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a/client: list skills: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a/client: list skills status %d", resp.StatusCode)
	}
	var out []SkillDescriptor
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("a2a/client: parse skills: %w", err)
	}
	return out, nil
}

// ExecuteSkill creates a task and polls until completion. Returns the
// artifact bytes on success.
func (c *Client) ExecuteSkill(ctx context.Context, skill string, input json.RawMessage) (json.RawMessage, error) {
	task, err := c.createTask(ctx, skill, input)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(c.pollMax)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return nil, errors.New("a2a/client: task did not complete within poll deadline")
		}
		latest, err := c.GetTask(ctx, task.ID)
		if err != nil {
			return nil, err
		}
		switch latest.State {
		case TaskCompleted:
			return latest.Artifact, nil
		case TaskFailed:
			return nil, fmt.Errorf("a2a/client: task failed: %s", latest.Error)
		}
		time.Sleep(c.pollEvery)
	}
}

// GetTask returns the current state of one task.
func (c *Client) GetTask(ctx context.Context, id string) (*Task, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/tasks/"+id, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a/client: get task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrTaskNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a/client: get task status %d", resp.StatusCode)
	}
	var t Task
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("a2a/client: parse task: %w", err)
	}
	return &t, nil
}

func (c *Client) createTask(ctx context.Context, skill string, input json.RawMessage) (*Task, error) {
	body, _ := json.Marshal(struct {
		Skill string          `json:"skill"`
		Input json.RawMessage `json:"input"`
	}{Skill: skill, Input: input})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/tasks", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a/client: create task: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a/client: create task status %d: %s", resp.StatusCode, string(raw))
	}
	var t Task
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("a2a/client: parse created task: %w", err)
	}
	return &t, nil
}
