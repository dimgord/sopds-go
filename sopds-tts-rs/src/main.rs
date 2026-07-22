use std::collections::HashMap;
use std::io::{BufRead, Read, Write};
use std::process::{Command, Stdio};
use std::time::Instant;

use hound::{SampleFormat, WavSpec, WavWriter};
use ort::session::Session;
use ort::value::Tensor;
use serde::Deserialize;

mod f5;
mod ruaccent; // native RUAccent stress port (Phase 1: dicts + preprocess) — replaces $RUPY

// A daemon serves one voice: Piper (a .onnx file) or native F5 (a directory of 3 ONNX graphs +
// vocab + reference). Same NDJSON protocol either way — the caller doesn't care which engine.
enum Engine {
    Piper(Tts),
    F5(f5::F5),
}

impl Engine {
    fn load(model_path: &str) -> Result<Self, String> {
        if f5::is_f5_dir(model_path) {
            Ok(Engine::F5(f5::F5::load(model_path)?))
        } else {
            Ok(Engine::Piper(Tts::load(model_path)?))
        }
    }
    fn sample_rate(&self) -> u32 {
        match self {
            Engine::Piper(t) => t.sample_rate(),
            Engine::F5(f) => f.sample_rate(),
        }
    }
    fn synth(&mut self, text: &str) -> Result<Vec<i16>, String> {
        match self {
            Engine::Piper(t) => t.synth(text),
            Engine::F5(f) => f.synth(text),
        }
    }
}

// Special phoneme IDs matching Piper convention.
const PAD_ID: i64 = 0; // "_"
const BOS_ID: i64 = 1; // "^"
const EOS_ID: i64 = 2; // "$"

// --- Piper config structs ---

#[derive(Deserialize)]
struct PiperConfig {
    audio: AudioConfig,
    #[serde(default)]
    espeak: EspeakConfig,
    #[serde(default)]
    inference: InferenceConfig,
    #[serde(default)]
    phoneme_type: Option<String>,
    phoneme_id_map: HashMap<String, Vec<i64>>,
    #[serde(default)]
    num_speakers: Option<i32>,
    #[serde(default)]
    speaker_id_map: Option<HashMap<String, i64>>,
}

#[derive(Deserialize)]
struct AudioConfig {
    sample_rate: u32,
}

#[derive(Deserialize, Default)]
struct EspeakConfig {
    #[serde(default)]
    voice: Option<String>,
}

#[derive(Deserialize, Default)]
struct InferenceConfig {
    #[serde(default = "default_noise_scale")]
    noise_scale: f32,
    #[serde(default = "default_length_scale")]
    length_scale: f32,
    #[serde(default = "default_noise_w")]
    noise_w: f32,
}

fn default_noise_scale() -> f32 {
    0.667
}
fn default_length_scale() -> f32 {
    1.0
}
fn default_noise_w() -> f32 {
    0.8
}

// --- Phonemization ---

fn text_to_phonemes(text: &str, config: &PiperConfig) -> Result<String, String> {
    let phoneme_type = config.phoneme_type.as_deref().unwrap_or("espeak");

    if phoneme_type == "text" {
        return Ok(text.to_lowercase());
    }

    let voice = config.espeak.voice.as_deref().unwrap_or("en");

    let mut child = Command::new("espeak-ng")
        .args(["--ipa", "-v", voice, "-q"])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| format!("failed to spawn espeak-ng: {e}"))?;

    if let Some(mut stdin) = child.stdin.take() {
        stdin
            .write_all(text.as_bytes())
            .map_err(|e| format!("failed to write to espeak-ng stdin: {e}"))?;
    }

    let output = child
        .wait_with_output()
        .map_err(|e| format!("espeak-ng failed: {e}"))?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        return Err(format!("espeak-ng exited with {}: {stderr}", output.status));
    }

    let phonemes = String::from_utf8_lossy(&output.stdout)
        .trim()
        .replace('\n', " ");

    Ok(phonemes)
}

// --- Phoneme-to-ID conversion ---

fn phonemes_to_ids(
    phonemes: &str,
    phoneme_map: &HashMap<char, Vec<i64>>,
) -> Result<Vec<i64>, String> {
    let mut ids = vec![BOS_ID, PAD_ID];

    for ch in phonemes.chars() {
        if let Some(phoneme_ids) = phoneme_map.get(&ch) {
            ids.extend(phoneme_ids);
            ids.push(PAD_ID);
        }
        // Skip unknown phonemes silently, matching Go behaviour.
    }

    ids.push(EOS_ID);

    if ids.len() <= 3 {
        return Err("no phoneme IDs generated".to_string());
    }

    Ok(ids)
}

// --- Build phoneme map ---

fn build_phoneme_map(raw: &HashMap<String, Vec<i64>>) -> HashMap<char, Vec<i64>> {
    let mut map = HashMap::new();
    for (phoneme, ids) in raw {
        let chars: Vec<char> = phoneme.chars().collect();
        if chars.len() == 1 {
            map.insert(chars[0], ids.clone());
        }
    }
    map
}

// --- TTS engine: load the model + config ONCE, synthesize many times ---
//
// The model load + ONNX session init is the dominant per-invocation cost
// (~0.3s for a 60MB Piper model); synthesis itself is ~15-70x real-time. So a
// resident process that loads once and synthesizes in a loop (daemon mode) is
// far faster per chunk than re-spawning per chunk.
struct Tts {
    session: Session,
    config: PiperConfig,
    phoneme_map: HashMap<char, Vec<i64>>,
}

impl Tts {
    fn load(model_path: &str) -> Result<Self, String> {
        let config_path = format!("{model_path}.json");
        let config_data = std::fs::read_to_string(&config_path)
            .map_err(|e| format!("failed to read config {config_path}: {e}"))?;
        let config: PiperConfig = serde_json::from_str(&config_data)
            .map_err(|e| format!("failed to parse config: {e}"))?;
        let phoneme_map = build_phoneme_map(&config.phoneme_id_map);

        let mut builder = Session::builder()
            .map_err(|e| format!("failed to create session builder: {e}"))?
            .with_execution_providers([
                // macOS: CPU. CoreML can't run this VITS/Piper model on the GPU —
                // its inputs have dynamic (unbounded) dimensions, which CoreML
                // MLProgram rejects, so the whole graph falls back to CPU anyway
                // (with heavy error spam). Apple-Silicon CPU is fast enough here.
                #[cfg(target_os = "macos")]
                ort::execution_providers::CPUExecutionProvider::default().build(),
                #[cfg(not(target_os = "macos"))]
                ort::execution_providers::CUDAExecutionProvider::default().build(),
            ])
            .map_err(|e| format!("failed to set execution providers: {e}"))?;

        // Optional intra-op thread cap. Lets a caller run several daemons in parallel
        // (e.g. fb2-to-wav.sh WORKERS=N) without each ORT session grabbing every core
        // and oversubscribing. Unset / 0 = ORT default (all cores).
        if let Some(n) = std::env::var("SOPDS_TTS_THREADS")
            .ok()
            .and_then(|v| v.parse::<usize>().ok())
            .filter(|&n| n > 0)
        {
            builder = builder
                .with_intra_threads(n)
                .map_err(|e| format!("failed to set intra threads: {e}"))?;
        }

        let session = builder
            .commit_from_file(model_path)
            .map_err(|e| format!("failed to load model {model_path}: {e}"))?;

        Ok(Self {
            session,
            config,
            phoneme_map,
        })
    }

    fn sample_rate(&self) -> u32 {
        self.config.audio.sample_rate
    }

    /// Synthesize `text` into 16-bit PCM samples.
    fn synth(&mut self, text: &str) -> Result<Vec<i16>, String> {
        let phonemes = text_to_phonemes(text, &self.config)?;
        let phoneme_ids = match phonemes_to_ids(&phonemes, &self.phoneme_map) {
            Ok(ids) => ids,
            // No pronounceable content (a "* * *" scene break, a bare number, punctuation) —
            // emit ~0.25s of silence instead of erroring, so one dud chunk can't fail a whole book.
            Err(_) => return Ok(vec![0i16; (self.sample_rate() / 4) as usize]),
        };
        let seq_len = phoneme_ids.len();

        let input_tensor = Tensor::from_array(([1, seq_len], phoneme_ids.into_boxed_slice()))
            .map_err(|e| format!("failed to create input tensor: {e}"))?;
        let lengths_tensor = Tensor::from_array(([1], vec![seq_len as i64].into_boxed_slice()))
            .map_err(|e| format!("failed to create lengths tensor: {e}"))?;
        let scales_tensor = Tensor::from_array((
            [3],
            vec![
                self.config.inference.noise_scale,
                self.config.inference.length_scale,
                self.config.inference.noise_w,
            ]
            .into_boxed_slice(),
        ))
        .map_err(|e| format!("failed to create scales tensor: {e}"))?;

        let num_speakers = self.config.num_speakers.unwrap_or(0);
        let outputs = if num_speakers > 1 {
            let speaker_id = self
                .config
                .speaker_id_map
                .as_ref()
                .and_then(|m| m.values().next().copied())
                .unwrap_or(0);
            let sid_tensor = Tensor::from_array(([1usize], vec![speaker_id].into_boxed_slice()))
                .map_err(|e| format!("failed to create sid tensor: {e}"))?;
            self.session
                .run(ort::inputs![
                    "input" => input_tensor,
                    "input_lengths" => lengths_tensor,
                    "scales" => scales_tensor,
                    "sid" => sid_tensor,
                ])
                .map_err(|e| format!("inference failed: {e}"))?
        } else {
            self.session
                .run(ort::inputs![
                    "input" => input_tensor,
                    "input_lengths" => lengths_tensor,
                    "scales" => scales_tensor,
                ])
                .map_err(|e| format!("inference failed: {e}"))?
        };

        let (_shape, audio_float) = outputs[0]
            .try_extract_tensor::<f32>()
            .map_err(|e| format!("failed to extract output tensor: {e}"))?;

        let audio_i16: Vec<i16> = audio_float
            .iter()
            .map(|&s: &f32| (s.clamp(-1.0, 1.0) * 32767.0) as i16)
            .collect();

        Ok(audio_i16)
    }
}

fn write_wav(samples: &[i16], output_path: &str, sample_rate: u32) -> Result<(), String> {
    let spec = WavSpec {
        channels: 1,
        sample_rate,
        bits_per_sample: 16,
        sample_format: SampleFormat::Int,
    };
    let mut writer = WavWriter::create(output_path, spec)
        .map_err(|e| format!("failed to create WAV file: {e}"))?;
    for &sample in samples {
        writer
            .write_sample(sample)
            .map_err(|e| format!("failed to write sample: {e}"))?;
    }
    writer
        .finalize()
        .map_err(|e| format!("failed to finalize WAV: {e}"))?;
    Ok(())
}

// --- Daemon mode: one NDJSON request per line on stdin, one response per line ---
//
//   request:  {"text": "...", "output": "/path/to/out.wav"}
//   response: {"ok": true,  "samples": 12345, "elapsed_ms": 42, "output": "..."}
//             {"ok": false, "error": "..."}
//
// The model is loaded once; each request only pays phonemize + inference + WAV.
#[derive(Deserialize)]
struct Request {
    text: String,
    output: String,
}

fn serve(model_path: &str) -> Result<(), String> {
    let mut engine = Engine::load(model_path)?;
    let sample_rate = engine.sample_rate();
    // Signal readiness on stderr so the parent knows the (slow) load is done.
    eprintln!("ready: {model_path} loaded (daemon mode; NDJSON on stdin)");

    let stdin = std::io::stdin();
    let mut reader = stdin.lock();
    let stdout = std::io::stdout();
    let mut line = String::new();

    loop {
        line.clear();
        let n = reader
            .read_line(&mut line)
            .map_err(|e| format!("failed to read request: {e}"))?;
        if n == 0 {
            exit_ok(); // EOF — parent closed stdin; exit before `tts` drops (ORT CUDA teardown crashes)
        }
        let trimmed = line.trim();
        if trimmed.is_empty() {
            continue;
        }

        let started = Instant::now();
        let resp = match serde_json::from_str::<Request>(trimmed) {
            Ok(req) => match engine.synth(&req.text).and_then(|samples| {
                write_wav(&samples, &req.output, sample_rate).map(|()| samples.len())
            }) {
                Ok(samples) => serde_json::json!({
                    "ok": true,
                    "samples": samples,
                    "elapsed_ms": started.elapsed().as_millis(),
                    "output": req.output,
                }),
                Err(e) => serde_json::json!({ "ok": false, "error": e }),
            },
            Err(e) => serde_json::json!({ "ok": false, "error": format!("bad request json: {e}") }),
        };

        let mut out = stdout.lock();
        writeln!(out, "{resp}").map_err(|e| format!("failed to write response: {e}"))?;
        out.flush().map_err(|e| format!("failed to flush response: {e}"))?;
    }
}

// --- Main ---

fn main() {
    if let Err(e) = run() {
        eprintln!("error: {e}");
        std::process::exit(1);
    }
    // On success run() exits the process itself (see `exit_ok` calls) *before* the ORT
    // Session is dropped — its CUDA-provider teardown corrupts the heap.
}

// exit_ok flushes stdout and terminates with status 0 without running destructors.
// ONNX Runtime's CUDA provider crashes ("corrupted double-linked list", core dump) when
// the Session is dropped at process exit — but only *after* all audio is written and
// flushed. We call this while the Session is still alive so its Drop never runs; the WAV
// is already finalized on disk and any daemon responses already sent. No-op-safe on the
// clean-exiting macOS/CPU path.
fn exit_ok() -> ! {
    let _ = std::io::stdout().flush();
    std::process::exit(0);
}

fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().collect();
    match args.len() {
        // One-shot (backward compatible): text on stdin -> one WAV, then exit.
        3 => {
            let model_path = &args[1];
            let output_path = &args[2];

            let mut text = String::new();
            std::io::stdin()
                .read_to_string(&mut text)
                .map_err(|e| format!("failed to read stdin: {e}"))?;
            let text = text.trim();
            if text.is_empty() {
                return Err("no input text".to_string());
            }

            let mut engine = Engine::load(model_path)?;
            let sample_rate = engine.sample_rate();
            // Do the work, then exit WITHOUT dropping `engine` on *either* path — the ORT CUDA
            // Session teardown corrupts the heap (Rev 79), and `?` would unwind-drop it on error.
            let result = engine.synth(text).and_then(|samples| {
                write_wav(&samples, output_path, sample_rate)?;
                eprintln!(
                    "generated {} samples ({:.1}s) at {sample_rate}Hz -> {output_path}",
                    samples.len(),
                    samples.len() as f64 / sample_rate as f64,
                );
                Ok(())
            });
            match result {
                Ok(()) => exit_ok(),
                Err(e) => {
                    eprintln!("error: {e}");
                    let _ = std::io::stdout().flush();
                    std::process::exit(1);
                }
            }
        }
        // Daemon: load model once, stream NDJSON requests on stdin.
        2 => serve(&args[1]),
        _ => Err(format!(
            "usage:\n  {0} <model_path> <output_path>   # one-shot (text on stdin)\n  {0} <model_path>                 # daemon (NDJSON requests on stdin)",
            args[0]
        )),
    }
}
