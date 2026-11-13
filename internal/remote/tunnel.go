package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tunnel forwards a local port to Target ("127.0.0.1:7878") as seen from
// Host, over the system ssh client. It starts on first use and restarts
// after the connection drops, so it can sit behind other routes as a
// fallback that costs nothing until needed.
type Tunnel struct {
	Host   string
	Target string

	mu     sync.Mutex
	addr   string
	cmd    *exec.Cmd
	done   chan struct{}
	stderr *bytes.Buffer
	closed bool
}

// Addr returns the local "127.0.0.1:port" of a running tunnel, starting
// (or restarting) it when needed.
func (t *Tunnel) Addr(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return "", errors.New("tunnel closed")
	}
	if t.cmd != nil {
		select {
		case <-t.done:
			t.cmd = nil // died: restart below
		default:
			return t.addr, nil
		}
	}
	return t.startLocked(ctx)
}

func (t *Tunnel) startLocked(ctx context.Context) (string, error) {
	port, err := freePort()
	if err != nil {
		return "", err
	}
	addr := "127.0.0.1:" + strconv.Itoa(port)
	cmd := exec.Command("ssh",
		"-N", "-T",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ConnectTimeout=10",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-L", addr+":"+t.Target,
		t.Host)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("ssh tunnel: %w", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	// Ready when the local port accepts connections.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			c.Close()
			break
		}
		select {
		case <-done:
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = "ssh exited"
			}
			return "", fmt.Errorf("ssh tunnel to %s: %s", t.Host, msg)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return "", ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			return "", fmt.Errorf("ssh tunnel to %s: timed out", t.Host)
		}
	}
	t.addr, t.cmd, t.done, t.stderr = addr, cmd, done, &stderr
	return addr, nil
}

// Close stops the tunnel for good.
func (t *Tunnel) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.cmd != nil {
		_ = t.cmd.Process.Kill()
		<-t.done
		t.cmd = nil
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
