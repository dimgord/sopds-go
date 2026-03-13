use std::collections::HashMap;
use std::io::Read;
use std::process::{Command, Stdio};

use hound::{SampleFormat, WavSpec, WavWriter};
use ort::session::Session;
use ort::value::Tensor;
use serde::Deserialize;

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
        use std::io::Write;
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

// --- Main ---

fn main() {
    if let Err(e) = run() {
        eprintln!("error: {e}");
        std::process::exit(1);
    }
}

fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        return Err(format!("usage: {} <model_path> <output_path>", args[0]));
    }
    let model_path = &args[1];
    let output_path = &args[2];

    // Read text from stdin.
    let mut text = String::new();
    std::io::stdin()
        .read_to_string(&mut text)
        .map_err(|e| format!("failed to read stdin: {e}"))?;
    let text = text.trim();
    if text.is_empty() {
        return Err("no input text".to_string());
    }

    // Load Piper config from <model_path>.json.
    let config_path = format!("{model_path}.json");
    let config_data = std::fs::read_to_string(&config_path)
        .map_err(|e| format!("failed to read config {config_path}: {e}"))?;
    let config: PiperConfig =
        serde_json::from_str(&config_data).map_err(|e| format!("failed to parse config: {e}"))?;

    // Build phoneme map (single-char keys only).
    let phoneme_map = build_phoneme_map(&config.phoneme_id_map);

    // Convert text to phonemes.
    let phonemes = text_to_phonemes(text, &config)?;

    // Convert phonemes to IDs.
    let phoneme_ids = phonemes_to_ids(&phonemes, &phoneme_map)?;
    let seq_len = phoneme_ids.len();

    // Create ONNX session with CUDA, falling back to CPU.
    let mut session = Session::builder()
        .map_err(|e| format!("failed to create session builder: {e}"))?
        .with_execution_providers([ort::execution_providers::CUDAExecutionProvider::default()
            .build()])
        .map_err(|e| format!("failed to set execution providers: {e}"))?
        .commit_from_file(model_path)
        .map_err(|e| format!("failed to load model {model_path}: {e}"))?;

    // Prepare input tensors.
    let input_tensor =
        Tensor::from_array(([1, seq_len], phoneme_ids.into_boxed_slice()))
            .map_err(|e| format!("failed to create input tensor: {e}"))?;

    let lengths_tensor =
        Tensor::from_array(([1], vec![seq_len as i64].into_boxed_slice()))
            .map_err(|e| format!("failed to create lengths tensor: {e}"))?;

    let scales_tensor = Tensor::from_array((
        [3],
        vec![
            config.inference.noise_scale,
            config.inference.length_scale,
            config.inference.noise_w,
        ]
        .into_boxed_slice(),
    ))
    .map_err(|e| format!("failed to create scales tensor: {e}"))?;

    // Build inputs.
    let num_speakers = config.num_speakers.unwrap_or(0);

    let outputs = if num_speakers > 1 {
        let speaker_id = config
            .speaker_id_map
            .as_ref()
            .and_then(|m| m.values().next().copied())
            .unwrap_or(0);
        let sid_tensor =
            Tensor::from_array(([1usize], vec![speaker_id].into_boxed_slice()))
                .map_err(|e| format!("failed to create sid tensor: {e}"))?;

        session
            .run(ort::inputs![
                "input" => input_tensor,
                "input_lengths" => lengths_tensor,
                "scales" => scales_tensor,
                "sid" => sid_tensor,
            ])
            .map_err(|e| format!("inference failed: {e}"))?
    } else {
        session
            .run(ort::inputs![
                "input" => input_tensor,
                "input_lengths" => lengths_tensor,
                "scales" => scales_tensor,
            ])
            .map_err(|e| format!("inference failed: {e}"))?
    };

    // Extract audio samples from output tensor.
    let (_shape, audio_float) = outputs[0]
        .try_extract_tensor::<f32>()
        .map_err(|e| format!("failed to extract output tensor: {e}"))?;

    // Convert float32 -> int16 PCM.
    let audio_i16: Vec<i16> = audio_float
        .iter()
        .map(|&s: &f32| {
            let clamped = s.clamp(-1.0, 1.0);
            (clamped * 32767.0) as i16
        })
        .collect();

    // Write WAV file.
    let spec = WavSpec {
        channels: 1,
        sample_rate: config.audio.sample_rate,
        bits_per_sample: 16,
        sample_format: SampleFormat::Int,
    };

    let mut writer = WavWriter::create(output_path, spec)
        .map_err(|e| format!("failed to create WAV file: {e}"))?;

    for &sample in &audio_i16 {
        writer
            .write_sample(sample)
            .map_err(|e| format!("failed to write sample: {e}"))?;
    }

    writer
        .finalize()
        .map_err(|e| format!("failed to finalize WAV: {e}"))?;

    eprintln!(
        "generated {} samples ({:.1}s) at {}Hz -> {output_path}",
        audio_i16.len(),
        audio_i16.len() as f64 / config.audio.sample_rate as f64,
        config.audio.sample_rate,
    );

    Ok(())
}
