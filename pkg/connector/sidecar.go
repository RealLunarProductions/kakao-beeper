package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// Sidecar runs sidecar/sidecar.mjs and speaks its newline-delimited JSON protocol.
type Sidecar struct {
	log     zerolog.Logger
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex

	nextID    atomic.Int64
	pendingMu sync.Mutex
	pending   map[int64]chan sidecarResponse

	// OnEvent is called from the reader goroutine for every pushed event.
	OnEvent func(name string, data json.RawMessage)
	// Done is closed when the process exits.
	Done chan struct{}
}

type sidecarResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *SidecarError   `json:"error"`
}

type sidecarLine struct {
	ID    *int64          `json:"id"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
	sidecarResponse
}

type SidecarError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *SidecarError) Error() string {
	return fmt.Sprintf("kakaotalk sidecar: %s (%s)", e.Message, e.Code)
}

var ErrSidecarExited = errors.New("kakaotalk sidecar exited")

func StartSidecar(cfg *Config, log zerolog.Logger, onEvent func(string, json.RawMessage)) (*Sidecar, error) {
	cmd := exec.Command(cfg.NodePath, cfg.SidecarScript)
	cmd.Env = append(os.Environ(), "KAKAO_SIDECAR_STATE_DIR="+cfg.StateDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start sidecar: %w", err)
	}
	sc := &Sidecar{
		log:     log,
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]chan sidecarResponse),
		OnEvent: onEvent,
		Done:    make(chan struct{}),
	}
	go sc.readStderr(stderr)
	go sc.readStdout(stdout)
	return sc, nil
}

func (sc *Sidecar) readStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		sc.log.Debug().Str("sidecar_stderr", scanner.Text()).Msg("Sidecar log")
	}
}

func (sc *Sidecar) readStdout(r io.Reader) {
	defer func() {
		_ = sc.cmd.Wait()
		sc.pendingMu.Lock()
		for id, ch := range sc.pending {
			close(ch)
			delete(sc.pending, id)
		}
		sc.pendingMu.Unlock()
		close(sc.Done)
	}()
	reader := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			sc.handleLine(line)
		}
		if err != nil {
			return
		}
	}
}

func (sc *Sidecar) handleLine(line []byte) {
	var msg sidecarLine
	if err := json.Unmarshal(line, &msg); err != nil {
		sc.log.Warn().Err(err).Msg("Failed to parse sidecar output")
		return
	}
	if msg.Event != "" {
		if sc.OnEvent != nil {
			sc.OnEvent(msg.Event, msg.Data)
		}
		return
	}
	if msg.ID == nil {
		return
	}
	sc.pendingMu.Lock()
	ch, ok := sc.pending[*msg.ID]
	delete(sc.pending, *msg.ID)
	sc.pendingMu.Unlock()
	if ok {
		ch <- msg.sidecarResponse
	}
}

// Call sends a request and decodes the result into out (if non-nil).
func (sc *Sidecar) Call(ctx context.Context, method string, params any, out any) error {
	id := sc.nextID.Add(1)
	ch := make(chan sidecarResponse, 1)
	sc.pendingMu.Lock()
	sc.pending[id] = ch
	sc.pendingMu.Unlock()
	defer func() {
		sc.pendingMu.Lock()
		delete(sc.pending, id)
		sc.pendingMu.Unlock()
	}()

	if params == nil {
		params = struct{}{}
	}
	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	sc.writeMu.Lock()
	_, err = sc.stdin.Write(append(payload, '\n'))
	sc.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("failed to write to sidecar: %w", err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return ErrSidecarExited
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	case <-sc.Done:
		return ErrSidecarExited
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop closes stdin, which makes the sidecar log out of LOCO and exit.
func (sc *Sidecar) Stop() {
	_ = sc.stdin.Close()
	select {
	case <-sc.Done:
	case <-time.After(10 * time.Second):
		_ = sc.cmd.Process.Kill()
		<-sc.Done
	}
}
