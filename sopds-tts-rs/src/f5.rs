//! Native F5-TTS engine (no Python) for the resident daemon — see docs/decisions/001.
//!
//! An F5 "model" is a **directory** containing the three exported ONNX graphs
//! (`F5_Preprocess.onnx`, `F5_Transformer.onnx`, `F5_Decode.onnx`), `vocab.txt`, and the voice
//! reference (`ref.wav` @ 24 kHz mono 16-bit + `ref.txt` its exact transcript). The daemon detects
//! F5 vs Piper by "is the path a directory". `synth(gen_text)` clones the fixed reference voice.
//!
//! The heavy math (attention, the flow-matching ODE, vocos) lives in the graphs; this tokenizes,
//! runs preprocess → NFE-1 transformer steps → vocos decode → i16 PCM. Verified against the DakeQQ
//! Python ONNX pipeline.
use std::collections::HashMap;

use ort::session::Session;
use ort::value::Tensor;

pub const SAMPLE_RATE: u32 = 24000;
const HOP: i64 = 256;
// Number of flow-matching ODE steps. More = higher fidelity but linearly slower (the transformer runs
// NFE_STEP-1 times per chunk — the dominant synth cost). Env `SOPDS_TTS_NFE` overrides; the auto-F5
// pipeline sets 16 (the F5 default, ~2x faster than 32) via fb2-to-f5.sh.
const NFE_STEP: i32 = 32;
const SPEED: f64 = 1.0;

pub struct F5 {
    pre: Session,
    transformer: Session,
    decode: Session,
    vocab: HashMap<char, i32>,
    ref_audio: Vec<i16>,
    ref_text: String,
    nfe_step: i32,
}

fn load_session(path: &str) -> Result<Session, String> {
    Session::builder()
        .map_err(|e| format!("builder: {e}"))?
        .with_execution_providers([
            #[cfg(target_os = "macos")]
            ort::execution_providers::CPUExecutionProvider::default().build(),
            #[cfg(not(target_os = "macos"))]
            ort::execution_providers::CUDAExecutionProvider::default().build(),
        ])
        .map_err(|e| format!("ep: {e}"))?
        .commit_from_file(path)
        .map_err(|e| format!("load {path}: {e}"))
}

/// True if `path` looks like an F5 model dir (has the three graphs).
pub fn is_f5_dir(path: &str) -> bool {
    let p = std::path::Path::new(path);
    p.is_dir() && p.join("F5_Transformer.onnx").exists()
}

impl F5 {
    pub fn load(dir: &str) -> Result<Self, String> {
        // vocab.txt: single-char line i -> {char: i}. .lines() strips \n AND a trailing \r — the
        // file is CRLF, which read_to_string keeps (unlike Python's text-mode open).
        let vocab_txt =
            std::fs::read_to_string(format!("{dir}/vocab.txt")).map_err(|e| format!("vocab: {e}"))?;
        let mut vocab = HashMap::new();
        for (i, line) in vocab_txt.lines().enumerate() {
            let mut chars = line.chars();
            if let Some(c) = chars.next() {
                if chars.next().is_none() {
                    vocab.insert(c, i as i32);
                }
            }
        }
        let ref_text = std::fs::read_to_string(format!("{dir}/ref.txt"))
            .map_err(|e| format!("ref.txt: {e}"))?
            .trim()
            .to_string();
        let mut r =
            hound::WavReader::open(format!("{dir}/ref.wav")).map_err(|e| format!("ref.wav: {e}"))?;
        let ref_audio = r
            .samples::<i16>()
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| format!("ref.wav read: {e}"))?;
        // NFE from env (SOPDS_TTS_NFE), clamped to ≥1; default NFE_STEP.
        let nfe_step = std::env::var("SOPDS_TTS_NFE")
            .ok()
            .and_then(|v| v.parse::<i32>().ok())
            .filter(|&n| n >= 1)
            .unwrap_or(NFE_STEP);
        Ok(Self {
            pre: load_session(&format!("{dir}/F5_Preprocess.onnx"))?,
            transformer: load_session(&format!("{dir}/F5_Transformer.onnx"))?,
            decode: load_session(&format!("{dir}/F5_Decode.onnx"))?,
            vocab,
            ref_audio,
            ref_text,
            nfe_step,
        })
    }

    pub fn sample_rate(&self) -> u32 {
        SAMPLE_RATE
    }

    fn tokenize(&self, gen_text: &str) -> Vec<i32> {
        // ref_text + gen_text; Russian = punctuation-normalize + char split (no jieba/pinyin).
        let norm = |c: char| match c {
            ';' => ',',
            '\u{201C}' | '\u{201D}' => '"',
            '\u{2018}' | '\u{2019}' => '\'',
            other => other,
        };
        // NB: NO space between ref_text and gen_text — the Russian model (Misha24) was trained on a
        // plain char-split (doc 001); inserting the convert_char_to_pinyin word-boundary space flattens
        // question intonation. (First-word clipping on very short chunks is handled by the chunker
        // keeping a sentence of context before a trailing "?"/"!" instead of isolating it.)
        (self.ref_text.chars().chain(gen_text.chars()))
            .map(norm)
            .map(|c| *self.vocab.get(&c).unwrap_or(&0))
            .collect()
    }

    pub fn synth(&mut self, gen_text: &str) -> Result<Vec<i16>, String> {
        let text_ids = self.tokenize(gen_text);
        let ref_audio_len = self.ref_audio.len() as i64 / HOP + 1;
        let ref_text_len = self.ref_text.len() as i64;
        let gen_text_len = gen_text.len() as i64;
        if gen_text_len == 0 {
            return Ok(vec![0i16; (SAMPLE_RATE / 4) as usize]); // empty chunk -> short silence
        }
        let max_duration = ref_audio_len
            + (ref_audio_len as f64 / ref_text_len as f64 * gen_text_len as f64 / SPEED) as i64;

        // Graph A: preprocess
        let audio_t =
            Tensor::from_array(([1usize, 1, self.ref_audio.len()], self.ref_audio.clone().into_boxed_slice()))
                .map_err(|e| format!("audio tensor: {e}"))?;
        let ids_t = Tensor::from_array(([1usize, text_ids.len()], text_ids.into_boxed_slice()))
            .map_err(|e| format!("ids tensor: {e}"))?;
        let md_t = Tensor::from_array(([1usize], vec![max_duration].into_boxed_slice()))
            .map_err(|e| format!("md tensor: {e}"))?;
        let a = self
            .pre
            .run(ort::inputs!["audio" => audio_t, "text_ids" => ids_t, "max_duration" => md_t])
            .map_err(|e| format!("preprocess: {e}"))?;

        let owned = |name: &str| -> Result<(Vec<i64>, Vec<f32>), String> {
            let (shape, data) = a[name]
                .try_extract_tensor::<f32>()
                .map_err(|e| format!("extract {name}: {e}"))?;
            Ok((shape.to_vec(), data.to_vec()))
        };
        let (mut noise_shape, mut noise) = {
            let n = owned("noise")?;
            (n.0, n.1)
        };
        let rope_cos_q = owned("rope_cos_q")?;
        let rope_sin_q = owned("rope_sin_q")?;
        let rope_cos_k = owned("rope_cos_k")?;
        let rope_sin_k = owned("rope_sin_k")?;
        let cat_mel_text = owned("cat_mel_text")?;
        let cat_mel_text_drop = owned("cat_mel_text_drop")?;
        let ref_signal_len = a["ref_signal_len"]
            .try_extract_tensor::<i64>()
            .map_err(|e| format!("extract rsl: {e}"))?
            .1[0];
        drop(a);

        let mk = |s: &(Vec<i64>, Vec<f32>)| -> Result<Tensor<f32>, String> {
            let shape: Vec<usize> = s.0.iter().map(|&d| d as usize).collect();
            Tensor::from_array((shape, s.1.clone().into_boxed_slice())).map_err(|e| format!("tensor: {e}"))
        };

        // Graph B: nfe_step-1 ODE steps (denoised -> noise, time_step threaded).
        let mut time_step: i32 = 0;
        for _ in 0..(self.nfe_step - 1) {
            let noise_t = {
                let shape: Vec<usize> = noise_shape.iter().map(|&d| d as usize).collect();
                Tensor::from_array((shape, noise.clone().into_boxed_slice())).map_err(|e| format!("noise: {e}"))?
            };
            let ts_t = Tensor::from_array(([1usize], vec![time_step].into_boxed_slice()))
                .map_err(|e| format!("ts: {e}"))?;
            let b = self
                .transformer
                .run(ort::inputs![
                    "noise" => noise_t,
                    "rope_cos_q" => mk(&rope_cos_q)?,
                    "rope_sin_q" => mk(&rope_sin_q)?,
                    "rope_cos_k" => mk(&rope_cos_k)?,
                    "rope_sin_k" => mk(&rope_sin_k)?,
                    "cat_mel_text" => mk(&cat_mel_text)?,
                    "cat_mel_text_drop" => mk(&cat_mel_text_drop)?,
                    "time_step.1" => ts_t,
                ])
                .map_err(|e| format!("transformer: {e}"))?;
            let (ds_shape, ds) = b["denoised"]
                .try_extract_tensor::<f32>()
                .map_err(|e| format!("extract denoised: {e}"))?;
            noise = ds.to_vec();
            noise_shape = ds_shape.to_vec();
            time_step = b["time_step"]
                .try_extract_tensor::<i32>()
                .map_err(|e| format!("extract ts: {e}"))?
                .1[0];
        }

        // Graph C: vocos decode -> i16 PCM
        let denoised_t = {
            let shape: Vec<usize> = noise_shape.iter().map(|&d| d as usize).collect();
            Tensor::from_array((shape, noise.into_boxed_slice())).map_err(|e| format!("denoised: {e}"))?
        };
        let rsl_t = Tensor::from_array((Vec::<usize>::new(), vec![ref_signal_len].into_boxed_slice()))
            .map_err(|e| format!("rsl tensor: {e}"))?;
        let c = self
            .decode
            .run(ort::inputs!["denoised" => denoised_t, "ref_signal_len" => rsl_t])
            .map_err(|e| format!("decode: {e}"))?;
        let (_, audio) = c["output_audio"]
            .try_extract_tensor::<i16>()
            .map_err(|e| format!("extract audio: {e}"))?;
        Ok(audio.to_vec())
    }
}
