//! Native F5-TTS runner (no Python) — the destination of docs/decisions/001.
//!
//! Loads the 3 exported ONNX graphs (F5_Preprocess / F5_Transformer / F5_Decode) + vocab.txt,
//! clones a reference voice, and synthesizes Russian text. All the heavy math (attention, the
//! flow-matching ODE, vocos) lives in the graphs; this just tokenizes, runs A → loops B → runs C.
//!
//!   f5 <onnx_dir> <vocab.txt> <ref.wav> <ref_text> <gen_text> <out.wav>
//!
//! Standalone for now (validated against the DakeQQ Python ONNX output); folds into the daemon later.
use std::collections::HashMap;

use hound::{SampleFormat, WavSpec, WavWriter};
use ort::session::Session;
use ort::value::Tensor;

const SAMPLE_RATE: u32 = 24000;
const HOP: i64 = 256;
const NFE_STEP: i32 = 32;
const SPEED: f64 = 1.0;

struct F5 {
    pre: Session,
    transformer: Session,
    decode: Session,
    vocab: HashMap<char, i32>,
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

impl F5 {
    fn load(onnx_dir: &str, vocab_path: &str) -> Result<Self, String> {
        let vocab_txt = std::fs::read_to_string(vocab_path).map_err(|e| format!("vocab: {e}"))?;
        // vocab.txt: single-char line i -> {char: i}. Use .lines() (strips \n AND a trailing \r) —
        // the file is CRLF, which Python's text-mode open() normalizes but read_to_string does not,
        // so split('\n') would leave '\r' and match no single char -> empty map -> all ids 0.
        let mut vocab = HashMap::new();
        for (i, line) in vocab_txt.lines().enumerate() {
            let mut chars = line.chars();
            if let Some(c) = chars.next() {
                if chars.next().is_none() {
                    vocab.insert(c, i as i32);
                }
            }
        }
        Ok(Self {
            pre: load_session(&format!("{onnx_dir}/F5_Preprocess.onnx"))?,
            transformer: load_session(&format!("{onnx_dir}/F5_Transformer.onnx"))?,
            decode: load_session(&format!("{onnx_dir}/F5_Decode.onnx"))?,
            vocab,
        })
    }

    /// ref_text + gen_text -> vocab ids (Russian: punctuation-normalize + char split; no jieba).
    fn tokenize(&self, ref_text: &str, gen_text: &str) -> Vec<i32> {
        let norm = |c: char| match c {
            ';' => ',',
            '\u{201C}' | '\u{201D}' => '"',
            '\u{2018}' | '\u{2019}' => '\'',
            other => other,
        };
        (ref_text.chars().chain(gen_text.chars()))
            .map(norm)
            .map(|c| *self.vocab.get(&c).unwrap_or(&0))
            .collect()
    }

    fn synth(&mut self, ref_audio: &[i16], ref_text: &str, gen_text: &str) -> Result<Vec<i16>, String> {
        let text_ids = self.tokenize(ref_text, gen_text);
        let ref_audio_len = ref_audio.len() as i64 / HOP + 1;
        let ref_text_len = ref_text.len() as i64; // utf8 byte length
        let gen_text_len = gen_text.len() as i64;
        let max_duration =
            ref_audio_len + (ref_audio_len as f64 / ref_text_len as f64 * gen_text_len as f64 / SPEED) as i64;

        // ---- Graph A: preprocess ----
        let audio_t = Tensor::from_array(([1usize, 1, ref_audio.len()], ref_audio.to_vec().into_boxed_slice()))
            .map_err(|e| format!("audio tensor: {e}"))?;
        let ids_t = Tensor::from_array(([1usize, text_ids.len()], text_ids.into_boxed_slice()))
            .map_err(|e| format!("ids tensor: {e}"))?;
        let md_t = Tensor::from_array(([1usize], vec![max_duration].into_boxed_slice()))
            .map_err(|e| format!("md tensor: {e}"))?;

        let a = self
            .pre
            .run(ort::inputs!["audio" => audio_t, "text_ids" => ids_t, "max_duration" => md_t])
            .map_err(|e| format!("preprocess: {e}"))?;

        // pull the constants (owned) that stay fixed across the B loop
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

        // ---- Graph B: NFE-step ODE loop (denoised -> noise, time_step threaded) ----
        // The graph's time schedule has NFE_STEP-1 entries and increments time_step itself, so it's
        // called NFE_STEP-1 times (Python: `range(0, NFE_STEP-1)`); a 32nd call indexes out of range.
        let mut time_step: i32 = 0;
        for _ in 0..(NFE_STEP - 1) {
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

        // ---- Graph C: decode (vocos) -> int16 PCM ----
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

fn read_wav_i16(path: &str) -> Result<Vec<i16>, String> {
    let mut r = hound::WavReader::open(path).map_err(|e| format!("open {path}: {e}"))?;
    // reference must already be 24 kHz mono 16-bit
    r.samples::<i16>().collect::<Result<Vec<_>, _>>().map_err(|e| format!("read: {e}"))
}

fn write_wav(samples: &[i16], path: &str) -> Result<(), String> {
    let spec = WavSpec { channels: 1, sample_rate: SAMPLE_RATE, bits_per_sample: 16, sample_format: SampleFormat::Int };
    let mut w = WavWriter::create(path, spec).map_err(|e| format!("create {path}: {e}"))?;
    for &s in samples {
        w.write_sample(s).map_err(|e| format!("write: {e}"))?;
    }
    w.finalize().map_err(|e| format!("finalize: {e}"))
}

fn main() {
    let a: Vec<String> = std::env::args().collect();
    if a.len() != 7 {
        eprintln!("usage: f5 <onnx_dir> <vocab.txt> <ref.wav> <ref_text> <gen_text> <out.wav>");
        std::process::exit(2);
    }
    let run = || -> Result<(), String> {
        let mut f5 = F5::load(&a[1], &a[2])?;
        let ref_audio = read_wav_i16(&a[3])?;
        let audio = f5.synth(&ref_audio, &a[4], &a[5])?;
        write_wav(&audio, &a[6])?;
        eprintln!("generated {} samples ({:.1}s) -> {}", audio.len(), audio.len() as f32 / SAMPLE_RATE as f32, a[6]);
        Ok(())
    };
    match run() {
        Ok(()) => std::process::exit(0), // skip ORT teardown (Rev 79)
        Err(e) => {
            eprintln!("error: {e}");
            std::process::exit(1);
        }
    }
}
