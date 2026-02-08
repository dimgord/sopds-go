package tts

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	ortInitOnce sync.Once
	ortInitErr  error
)

// Piper implements text-to-speech using piper ONNX models
type Piper struct {
	session    *ort.DynamicAdvancedSession
	config     *PiperConfig
	phonemeMap map[rune][]int64
	speakerID  int64 // speaker ID for multi-speaker models
}

// PiperConfig holds the voice model configuration
type PiperConfig struct {
	Audio struct {
		SampleRate int    `json:"sample_rate"`
		Quality    string `json:"quality"`
	} `json:"audio"`
	Espeak struct {
		Voice string `json:"voice"`
	} `json:"espeak"`
	Inference struct {
		NoiseScale  float32 `json:"noise_scale"`
		LengthScale float32 `json:"length_scale"`
		NoiseW      float32 `json:"noise_w"`
	} `json:"inference"`
	PhonemeType    string            `json:"phoneme_type"`     // "espeak" (default) or "text"
	PhonemeIDMap   map[string][]int  `json:"phoneme_id_map"`
	NumSpeakers    int               `json:"num_speakers"`
	SpeakerIDMap   map[string]int    `json:"speaker_id_map"`
}

// Special phoneme IDs
const (
	padID = 0 // "_"
	bosID = 1 // "^" - beginning of sequence
	eosID = 2 // "$" - end of sequence
)

// initORT initializes ONNX Runtime once
func initORT() error {
	ortInitOnce.Do(func() {
		ort.SetSharedLibraryPath("/usr/lib64/libonnxruntime.so")
		ortInitErr = ort.InitializeEnvironment()
	})
	return ortInitErr
}

// NewPiper creates a new Piper TTS instance
func NewPiper(modelPath string) (*Piper, error) {
	// Initialize ONNX Runtime
	if err := initORT(); err != nil {
		return nil, fmt.Errorf("failed to init onnxruntime: %w", err)
	}

	// Load config
	configPath := modelPath + ".json"
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config PiperConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Build phoneme map (rune -> []int64)
	phonemeMap := make(map[rune][]int64)
	for phoneme, ids := range config.PhonemeIDMap {
		runes := []rune(phoneme)
		if len(runes) == 1 {
			int64IDs := make([]int64, len(ids))
			for i, id := range ids {
				int64IDs[i] = int64(id)
			}
			phonemeMap[runes[0]] = int64IDs
		}
	}

	// Create ONNX session with dynamic shapes
	inputNames := []string{"input", "input_lengths", "scales"}
	if config.NumSpeakers > 1 {
		inputNames = append(inputNames, "sid")
	}
	outputNames := []string{"output"}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		inputNames,
		outputNames,
		nil, // Default options
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Default speaker ID = 0 (first speaker)
	var speakerID int64
	if config.NumSpeakers > 1 {
		// Use first speaker from map, or 0
		for _, id := range config.SpeakerIDMap {
			speakerID = int64(id)
			break
		}
	}

	return &Piper{
		session:    session,
		config:     &config,
		phonemeMap: phonemeMap,
		speakerID:  speakerID,
	}, nil
}

// Close releases resources
func (p *Piper) Close() error {
	var err error
	if p.session != nil {
		err = p.session.Destroy()
		p.session = nil
	}
	// Clear references to help GC
	p.config = nil
	p.phonemeMap = nil
	return err
}

// SampleRate returns the audio sample rate
func (p *Piper) SampleRate() int {
	return p.config.Audio.SampleRate
}

// Synthesize converts text to audio samples
func (p *Piper) Synthesize(text string) ([]int16, error) {
	// Get phonemes from espeak-ng
	phonemes, err := p.textToPhonemes(text)
	if err != nil {
		return nil, fmt.Errorf("phonemization failed: %w", err)
	}

	// Convert phonemes to IDs
	ids := p.phonemesToIDs(phonemes)
	if len(ids) == 0 {
		return nil, fmt.Errorf("no phoneme IDs generated")
	}

	// Run inference
	audio, err := p.runInference(ids)
	if err != nil {
		return nil, fmt.Errorf("inference failed: %w", err)
	}

	return audio, nil
}

// textToPhonemes converts text to phonemes
// For "text" phoneme_type models, returns lowercase text directly (no espeak-ng)
// For standard models, uses espeak-ng to convert text to IPA phonemes
func (p *Piper) textToPhonemes(text string) (string, error) {
	if p.config.PhonemeType == "text" {
		// Text-based model: use raw text as "phonemes" (lowercase)
		return strings.ToLower(text), nil
	}

	voice := p.config.Espeak.Voice
	if voice == "" {
		voice = "en"
	}

	cmd := exec.Command("espeak-ng", "--ipa", "-v", voice, "-q")
	cmd.Stdin = strings.NewReader(text)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("espeak-ng failed: %w", err)
	}

	// Clean up output - remove newlines and extra spaces
	phonemes := strings.TrimSpace(string(output))
	phonemes = strings.ReplaceAll(phonemes, "\n", " ")

	return phonemes, nil
}

// phonemesToIDs converts phoneme string to ID sequence
func (p *Piper) phonemesToIDs(phonemes string) []int64 {
	var ids []int64

	// BOS + PAD
	ids = append(ids, bosID, padID)

	// Convert each phoneme character
	for _, r := range phonemes {
		if phonemeIDs, ok := p.phonemeMap[r]; ok {
			ids = append(ids, phonemeIDs...)
			ids = append(ids, padID) // PAD after each phoneme
		}
		// Skip unknown phonemes silently
	}

	// EOS
	ids = append(ids, eosID)

	return ids
}

// runInference runs the ONNX model
func (p *Piper) runInference(phonemeIDs []int64) ([]int16, error) {
	// Prepare input tensor: shape [1, seq_len]
	inputShape := ort.NewShape(1, int64(len(phonemeIDs)))
	inputTensor, err := ort.NewTensor(inputShape, phonemeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	// Input lengths tensor: shape [1]
	lengthsShape := ort.NewShape(1)
	lengthsTensor, err := ort.NewTensor(lengthsShape, []int64{int64(len(phonemeIDs))})
	if err != nil {
		return nil, fmt.Errorf("failed to create lengths tensor: %w", err)
	}
	defer lengthsTensor.Destroy()

	// Scales tensor: shape [3] = [noise_scale, length_scale, noise_w]
	scalesShape := ort.NewShape(3)
	scales := []float32{
		p.config.Inference.NoiseScale,
		p.config.Inference.LengthScale,
		p.config.Inference.NoiseW,
	}
	scalesTensor, err := ort.NewTensor(scalesShape, scales)
	if err != nil {
		return nil, fmt.Errorf("failed to create scales tensor: %w", err)
	}
	defer scalesTensor.Destroy()

	inputs := []ort.Value{inputTensor, lengthsTensor, scalesTensor}

	// Multi-speaker models require a sid (speaker ID) input
	if p.config.NumSpeakers > 1 {
		sidShape := ort.NewShape(1)
		sidTensor, err := ort.NewTensor(sidShape, []int64{p.speakerID})
		if err != nil {
			return nil, fmt.Errorf("failed to create sid tensor: %w", err)
		}
		defer sidTensor.Destroy()
		inputs = append(inputs, sidTensor)
	}

	// Run inference with nil output to let ONNX runtime allocate
	outputs := []ort.Value{nil}

	err = p.session.Run(inputs, outputs)
	if err != nil {
		return nil, fmt.Errorf("inference run failed: %w", err)
	}
	defer outputs[0].Destroy()

	// Get output as float32 tensor
	outputTensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("output is not float32 tensor")
	}

	// Get output audio
	audioFloat := outputTensor.GetData()

	// Convert float32 to int16
	audio := make([]int16, len(audioFloat))
	for i, sample := range audioFloat {
		// Clip to [-1, 1] and scale to int16 range
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		audio[i] = int16(sample * 32767)
	}

	return audio, nil
}

// SynthesizeToFile generates audio and saves to WAV file
func (p *Piper) SynthesizeToFile(text, outputPath string) error {
	audio, err := p.Synthesize(text)
	if err != nil {
		return err
	}

	return WriteWAV(outputPath, audio, p.SampleRate())
}

// WriteWAV writes audio samples to a WAV file
func WriteWAV(path string, samples []int16, sampleRate int) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return EncodeWAV(f, samples, sampleRate)
}

// EncodeWAV writes WAV format to a writer
func EncodeWAV(w io.Writer, samples []int16, sampleRate int) error {
	numChannels := 1
	bitsPerSample := 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(samples) * 2

	// RIFF header
	w.Write([]byte("RIFF"))
	binary.Write(w, binary.LittleEndian, uint32(36+dataSize))
	w.Write([]byte("WAVE"))

	// fmt chunk
	w.Write([]byte("fmt "))
	binary.Write(w, binary.LittleEndian, uint32(16))         // chunk size
	binary.Write(w, binary.LittleEndian, uint16(1))          // audio format (PCM)
	binary.Write(w, binary.LittleEndian, uint16(numChannels))
	binary.Write(w, binary.LittleEndian, uint32(sampleRate))
	binary.Write(w, binary.LittleEndian, uint32(byteRate))
	binary.Write(w, binary.LittleEndian, uint16(blockAlign))
	binary.Write(w, binary.LittleEndian, uint16(bitsPerSample))

	// data chunk
	w.Write([]byte("data"))
	binary.Write(w, binary.LittleEndian, uint32(dataSize))

	// Write samples
	for _, sample := range samples {
		binary.Write(w, binary.LittleEndian, sample)
	}

	return nil
}
