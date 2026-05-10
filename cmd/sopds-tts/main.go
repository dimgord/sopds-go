// cmd/sopds-tts is the standalone TTS subprocess invoked by sopds when
// audio generation is requested. It depends on `internal/tts/piper.go`
// which transitively imports `github.com/yalue/onnxruntime_go` (CGO).
// Under CGO_ENABLED=0 this file is excluded from the build (the
// release/Docker/Homebrew matrices); under CGO_ENABLED=1 (default for
// `go install` / source builds) it compiles normally. Use the Rust
// port (`sopds-tts-rs/`) for CUDA-accelerated TTS.
//
//go:build cgo

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/dimgord/sopds-go/internal/tts"
)

// sopds-tts is a subprocess helper for TTS generation
// It processes a single text chunk and exits, guaranteeing memory release
//
// Usage: sopds-tts <model_path> <output_path>
// Text is read from stdin

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <model_path> <output_path>\n", os.Args[0])
		os.Exit(1)
	}

	modelPath := os.Args[1]
	outputPath := os.Args[2]

	// Read text from stdin
	textBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read stdin: %v\n", err)
		os.Exit(1)
	}
	text := string(textBytes)

	if text == "" {
		fmt.Fprintf(os.Stderr, "No text provided\n")
		os.Exit(1)
	}

	// Create Piper instance
	piper, err := tts.NewPiper(modelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create piper: %v\n", err)
		os.Exit(1)
	}
	defer piper.Close()

	// Synthesize to file
	if err := piper.SynthesizeToFile(text, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Synthesis failed: %v\n", err)
		os.Exit(1)
	}

	// Success - process exits, all memory released
}
