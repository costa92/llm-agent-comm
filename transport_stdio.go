package comm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// StdioTransportOptions configures StdioTransport.
type StdioTransportOptions struct {
	Env        []string      // additional env vars (KEY=VAL)
	WorkingDir string        // process cwd; default = inherit
	StartupTimeout time.Duration // max time to wait for first response after Start; default 5s
}

// StdioTransport runs a child process and exchanges JSON Envelopes
// over its stdin/stdout (one JSON document per line). MCP stdio
// servers (e.g. `npx @modelcontextprotocol/server-filesystem`)
// follow this convention.
//
// Note: requests are serialized via mu; a multi-RPC server that wants
// concurrent in-flight calls would need request-ID-keyed dispatch.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// NewStdioTransport launches command with args and returns a ready
// transport. Returns the start-up error if the process fails to spawn.
func NewStdioTransport(command string, args []string, opts StdioTransportOptions) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	if opts.WorkingDir != "" {
		cmd.Dir = opts.WorkingDir
	}
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Environ(), opts.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("comm/stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("comm/stdio: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("comm/stdio: start %q: %w", command, err)
	}
	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Call implements Transport. Writes Envelope as a single JSON line +
// newline; reads one JSON line back.
func (t *StdioTransport) Call(ctx context.Context, env Envelope) (Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return Response{}, ErrTransportClosed
	}

	body, err := json.Marshal(env)
	if err != nil {
		return Response{}, fmt.Errorf("comm/stdio: marshal envelope: %w", err)
	}
	body = append(body, '\n')

	// Use a goroutine-based ctx-aware write so a hanging process honors cancellation.
	writeDone := make(chan error, 1)
	go func() {
		_, werr := t.stdin.Write(body)
		writeDone <- werr
	}()
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case werr := <-writeDone:
		if werr != nil {
			return Response{}, fmt.Errorf("comm/stdio: write: %w", werr)
		}
	}

	type readResult struct {
		line []byte
		err  error
	}
	readDone := make(chan readResult, 1)
	go func() {
		line, rerr := t.reader.ReadBytes('\n')
		readDone <- readResult{line: line, err: rerr}
	}()
	select {
	case <-ctx.Done():
		return Response{}, ctx.Err()
	case r := <-readDone:
		if r.err != nil {
			return Response{}, fmt.Errorf("comm/stdio: read: %w", r.err)
		}
		var resp Response
		if err := json.Unmarshal(r.line, &resp); err != nil {
			return Response{}, fmt.Errorf("comm/stdio: unmarshal: %w (line=%s)", err, truncate(r.line, 200))
		}
		if resp.ID == "" {
			resp.ID = env.ID
		}
		return resp, nil
	}
}

// Close implements Transport. Closes stdin and waits for the process
// to exit.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	_ = t.stdin.Close()
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	return nil
}
