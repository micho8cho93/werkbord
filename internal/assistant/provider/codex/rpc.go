package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"devboard/internal/assistant/provider/cli"
)

// message is one JSON-RPC line, in either direction.
type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

// handlers receive what the server sends on its own. They run on the reader's goroutine and must not block.
type handlers struct {
	notification func(method string, params json.RawMessage)
	request      func(c *client, id json.RawMessage, method string, params json.RawMessage)
}

// client is a JSON-RPC conversation with one app-server process.
type client struct {
	w      *cli.Writer
	cancel context.CancelFunc
	done   chan struct{}
	h      handlers

	mu    sync.Mutex
	next  int64
	calls map[int64]chan message
	exit  cli.Exit
	err   error
}

// dial starts an app-server and the handshake that opens it. (initialize, then the initialized notification.)
func dial(ctx context.Context, path string, args []string, dir string) (*client, error) {
	return start(ctx, path, args, dir, handlers{})
}

func start(ctx context.Context, path string, args []string, dir string, h handlers) (*client, error) {
	runCtx, cancel := context.WithCancel(ctx)
	c := &client{cancel: cancel, done: make(chan struct{}), h: h, calls: map[int64]chan message{}}
	ready := make(chan struct{})
	go func() {
		defer close(c.done)
		exit, err := cli.RunInteractive(runCtx, cli.Spec{Path: path, Args: args, Dir: dir, Interactive: true},
			func(w *cli.Writer) { c.w = w; close(ready) }, c.line)
		c.mu.Lock()
		c.exit, c.err = exit, err
		c.mu.Unlock()
	}()
	select {
	case <-ready:
	case <-c.done:
		c.cancel()
		return nil, c.failure()
	}
	err := c.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "werkbord-assistant", "title": "Werkbord assistant", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil)
	if err == nil {
		err = c.notify("initialized")
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return fmt.Errorf("codex exited (code %d): %s", c.exit.Code, c.exit.Stderr)
}

// close ends the process and waits for it.
func (c *client) close() {
	if c.w != nil {
		c.w.Close()
	}
	c.cancel()
	<-c.done
}

func (c *client) line(raw []byte, _ *cli.Writer) bool {
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		return true // a stray line
	}
	switch {
	case m.Method != "" && len(m.ID) > 0:
		if c.h.request != nil {
			c.h.request(c, m.ID, m.Method, m.Params)
		} else {
			_ = c.respondError(m.ID, -32601, "not supported")
		}
	case m.Method != "":
		if c.h.notification != nil {
			c.h.notification(m.Method, m.Params)
		}
	case len(m.ID) > 0:
		var id int64
		if json.Unmarshal(m.ID, &id) != nil {
			return true
		}
		c.mu.Lock()
		ch := c.calls[id]
		c.mu.Unlock()
		if ch != nil {
			ch <- m
		}
	}
	return true
}

func (c *client) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.w.WriteLine(b)
}

func (c *client) notify(method string) error { return c.send(map[string]any{"method": method}) }

func (c *client) respondError(id json.RawMessage, code int, msg string) error {
	return c.send(map[string]any{"id": id, "error": map[string]any{"code": code, "message": msg}})
}

// call sends a request and waits for its response, the end of the process, or ctx.
func (c *client) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan message, 1)
	c.calls[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.calls, id)
		c.mu.Unlock()
	}()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		select {
		case <-c.done:
			return c.failure()
		default:
		}
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return fmt.Errorf("%s: %w", method, m.Error)
		}
		if out != nil && len(m.Result) > 0 {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	case <-c.done:
		return c.failure()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isRPC reports whether err is an error the server answered with, as opposed to the process failing.
func isRPC(err error) (*rpcError, bool) {
	var e *rpcError
	return e, errors.As(err, &e)
}
