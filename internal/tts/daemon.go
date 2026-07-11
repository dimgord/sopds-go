package tts

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Resident-model TTS. The one-shot contract `sopds-tts <model> <output>` reloads the
// ~60MB ONNX model on every chunk (~0.34s of the ~0.4s total). Daemon mode —
// `sopds-tts <model>` with no output arg — loads the model once and answers NDJSON
// requests on stdin (~20-90ms per chunk). See sopds-tts-rs Rev 77.
//
// A daemonPool keeps up to `size` live daemons per model, reused across chunks and
// jobs. If a daemon can't start (old binary without daemon mode, espeak missing), the
// model is marked broken and callers transparently fall back to one-shot subprocesses.

// errDaemonUnsupported signals the caller to fall back to a one-shot subprocess.
var errDaemonUnsupported = errors.New("tts daemon mode unavailable")

type daemonRequest struct {
	Text   string `json:"text"`
	Output string `json:"output"`
}

type daemonResponse struct {
	OK        bool   `json:"ok"`
	Samples   int    `json:"samples"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Output    string `json:"output"`
	Error     string `json:"error"`
}

// daemon is a single resident sopds-tts process. One request/response at a time.
type daemon struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex // serializes the NDJSON request/response on the pipe
}

// startDaemon launches `binary <modelPath>` and waits for its `ready:` line on stderr.
func startDaemon(binary, modelPath string) (*daemon, error) {
	cmd := exec.Command(binary, modelPath) // no output arg => daemon mode
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
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	name := filepath.Base(modelPath)
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		got := false
		for sc.Scan() {
			line := sc.Text()
			if !got && strings.HasPrefix(line, "ready:") {
				got = true
				ready <- nil
				continue
			}
			log.Printf("TTS daemon [%s]: %s", name, line)
		}
		if !got {
			ready <- fmt.Errorf("daemon exited before ready")
		}
	}()

	select {
	case err := <-ready:
		if err != nil {
			cmd.Wait()
			return nil, err
		}
	case <-time.After(60 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		return nil, fmt.Errorf("daemon startup timed out")
	}
	return &daemon{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

// synth sends one chunk and waits for its WAV. Text may contain newlines — JSON
// encoding escapes them so the request stays a single NDJSON line.
func (d *daemon) synth(text, output string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	req, err := json.Marshal(daemonRequest{Text: text, Output: output})
	if err != nil {
		return err
	}
	if _, err := d.stdin.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("daemon write: %w", err)
	}
	line, err := d.stdout.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("daemon read: %w", err)
	}
	var resp daemonResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("daemon bad response %q: %w", strings.TrimSpace(string(line)), err)
	}
	if !resp.OK {
		return fmt.Errorf("daemon synth failed: %s", resp.Error)
	}
	return nil
}

func (d *daemon) close() {
	d.stdin.Close() // EOF => the daemon exits its read loop
	if d.cmd.Process != nil {
		d.cmd.Process.Kill()
	}
	d.cmd.Wait()
}

// modelPool holds up to max live daemons for one model, created lazily.
type modelPool struct {
	idle    chan *daemon
	mu      sync.Mutex
	created int
	max     int
	broken  bool // startDaemon failed once => fall back to one-shot for this model
}

func (mp *modelPool) get(binary, modelPath string) (*daemon, error) {
	// Fast path: an idle daemon is waiting.
	select {
	case d := <-mp.idle:
		return d, nil
	default:
	}

	mp.mu.Lock()
	if mp.broken {
		mp.mu.Unlock()
		return nil, errDaemonUnsupported
	}
	if mp.created < mp.max {
		mp.created++
		mp.mu.Unlock()
		d, err := startDaemon(binary, modelPath)
		if err != nil {
			mp.mu.Lock()
			mp.created--
			mp.broken = true
			mp.mu.Unlock()
			log.Printf("TTS: daemon mode unavailable for %s (%v); using one-shot subprocess", filepath.Base(modelPath), err)
			return nil, errDaemonUnsupported
		}
		return d, nil
	}
	mp.mu.Unlock()

	// Pool is at capacity — wait for a busy daemon to come back.
	return <-mp.idle, nil
}

func (mp *modelPool) put(d *daemon) { mp.idle <- d }

// discard drops a dead daemon so get() can spawn a fresh one in its place.
func (mp *modelPool) discard(d *daemon) {
	d.close()
	mp.mu.Lock()
	mp.created--
	mp.mu.Unlock()
}

// daemonPool is the per-generator registry of resident TTS processes, keyed by model.
type daemonPool struct {
	binary string
	max    int
	mu     sync.Mutex
	pools  map[string]*modelPool
}

func newDaemonPool(binary string, max int) *daemonPool {
	if max < 1 {
		max = 1
	}
	return &daemonPool{binary: binary, max: max, pools: make(map[string]*modelPool)}
}

func (p *daemonPool) poolFor(modelPath string) *modelPool {
	p.mu.Lock()
	defer p.mu.Unlock()
	mp := p.pools[modelPath]
	if mp == nil {
		mp = &modelPool{idle: make(chan *daemon, p.max), max: p.max}
		p.pools[modelPath] = mp
	}
	return mp
}

// synth generates one chunk via a resident daemon. Returns errDaemonUnsupported when
// the caller should fall back to a one-shot subprocess.
func (p *daemonPool) synth(modelPath, text, output string) error {
	mp := p.poolFor(modelPath)
	d, err := mp.get(p.binary, modelPath)
	if err != nil {
		return err // errDaemonUnsupported or a real start error
	}
	if err := d.synth(text, output); err != nil {
		mp.discard(d) // pipe/process is likely dead; don't reuse it
		return err
	}
	mp.put(d)
	return nil
}

// shutdown stops every resident daemon.
func (p *daemonPool) shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, mp := range p.pools {
		for {
			select {
			case d := <-mp.idle:
				d.close()
			default:
				goto next
			}
		}
	next:
	}
	p.pools = make(map[string]*modelPool)
}
