package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestDaemonPool exercises the resident-model path against a real sopds-tts binary and
// model. It is skipped unless both env vars are set (and espeak-ng is on PATH):
//
//	SOPDS_TTS_BIN=.../sopds-tts-rs SOPDS_TTS_MODEL=.../voice.onnx \
//	  nix shell nixpkgs#espeak-ng -c go test ./internal/tts/ -run Daemon -v
func TestDaemonPool(t *testing.T) {
	bin := os.Getenv("SOPDS_TTS_BIN")
	model := os.Getenv("SOPDS_TTS_MODEL")
	if bin == "" || model == "" {
		t.Skip("set SOPDS_TTS_BIN and SOPDS_TTS_MODEL to run the daemon integration test")
	}

	pool := newDaemonPool(bin, 2)
	defer pool.shutdown()

	texts := []string{
		"First sentence, synthesized through the resident daemon.",
		"A second, somewhat longer sentence to check the pipe stays in sync.",
		"Third.",
		"The fourth and final sentence of the daemon integration test.",
	}

	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make([]error, len(texts))
	start := time.Now()
	for i, text := range texts {
		wg.Add(1)
		go func(i int, text string) {
			defer wg.Done()
			out := filepath.Join(dir, fmt.Sprintf("chunk_%d.wav", i))
			if err := pool.synth(model, text, out); err != nil {
				errs[i] = err
				return
			}
			info, err := os.Stat(out)
			if err != nil {
				errs[i] = fmt.Errorf("output missing: %w", err)
				return
			}
			if info.Size() < 1024 { // a real WAV is far bigger than the 44-byte header
				errs[i] = fmt.Errorf("output too small: %d bytes", info.Size())
			}
		}(i, text)
	}
	wg.Wait()
	elapsed := time.Since(start)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("chunk %d failed: %v", i, err)
		}
	}
	// 4 chunks with 2 daemons and the model loaded once — should be well under the
	// ~1.4s it would take as 4 separate one-shot processes (~0.34s reload each).
	t.Logf("synthesized %d chunks in %v (model loaded once, pool size 2)", len(texts), elapsed)
}
