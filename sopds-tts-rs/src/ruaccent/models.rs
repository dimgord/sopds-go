//! The four ONNX models (accent, stress_usage, yo_homograph, omograph) via `ort` — the same
//! onnxruntime C++ lib the Python side uses, so logits are bit-identical given identical tokenization.
//! Ported from ruaccent/{accent,stress_usage,yo_homograph,omograph}_model.py.

use std::borrow::Cow;
use std::io;
use std::path::Path;

use ort::session::Session;
use ort::value::{Tensor, Value};

use super::char_tok::CharTokenizer;
use super::ner;
use super::tok_bert::BertTokenizer;

fn load_session(path: &Path) -> io::Result<Session> {
    Session::builder()
        .and_then(|b| {
            b.with_execution_providers([
                #[cfg(target_os = "macos")]
                ort::execution_providers::CPUExecutionProvider::default().build(),
                #[cfg(not(target_os = "macos"))]
                ort::execution_providers::CUDAExecutionProvider::default().build(),
            ])
        })
        .and_then(|b| b.commit_from_file(path))
        .map_err(|e| io::Error::other(format!("load {}: {e}", path.display())))
}

/// id2label as a dense Vec indexed by class id (config.json `id2label` is `{"0": "...", ...}`).
fn load_id2label(dir: &Path) -> io::Result<Vec<String>> {
    let v: serde_json::Value = serde_json::from_reader(std::fs::File::open(dir.join("config.json"))?)?;
    let map = v.get("id2label").and_then(|m| m.as_object()).ok_or_else(|| {
        io::Error::new(io::ErrorKind::InvalidData, "config.json has no id2label object")
    })?;
    let mut labels = vec![String::new(); map.len()];
    for (k, val) in map {
        let i: usize = k.parse().map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "bad id2label key"))?;
        if i >= labels.len() {
            labels.resize(i + 1, String::new());
        }
        labels[i] = val.as_str().unwrap_or_default().to_string();
    }
    Ok(labels)
}

/// Build the model feed from its OWN declared inputs (robust to a model that omits `token_type_ids`,
/// e.g. yo/omograph) and run it, returning the `logits` tensor `(shape, data)`. `input_ids`/`attn`/
/// `ttype` are row-major `[batch, seq]`.
fn run_logits(
    session: &mut Session,
    input_ids: &[i64],
    attn: &[i64],
    ttype: &[i64],
    batch: usize,
    seq: usize,
) -> io::Result<(Vec<i64>, Vec<f32>)> {
    let shape = [batch, seq];
    let mk = |data: &[i64]| -> io::Result<Value> {
        Tensor::from_array((shape, data.to_vec().into_boxed_slice()))
            .map(Value::from)
            .map_err(|e| io::Error::other(format!("tensor: {e}")))
    };
    let mut feed: Vec<(Cow<str>, Value)> = Vec::new();
    for inp in &session.inputs {
        let name = inp.name.clone();
        let t = match name.as_str() {
            "attention_mask" => mk(attn)?,
            "token_type_ids" => mk(ttype)?,
            _ => mk(input_ids)?, // input_ids (and any unexpected extra input → zeros-shaped ids)
        };
        feed.push((Cow::Owned(name), t));
    }
    let outputs = session.run(feed).map_err(|e| io::Error::other(format!("run: {e}")))?;
    let (shape, data) = outputs["logits"]
        .try_extract_tensor::<f32>()
        .map_err(|e| io::Error::other(format!("extract logits: {e}")))?;
    Ok((shape.to_vec(), data.to_vec()))
}

/// Numerically-stable softmax over the last axis of a `[..., classes]` row (matches numpy
/// `exp(x-max)/sum`).
pub fn softmax(row: &[f32]) -> Vec<f32> {
    let m = row.iter().cloned().fold(f32::NEG_INFINITY, f32::max);
    let exps: Vec<f32> = row.iter().map(|&x| (x - m).exp()).collect();
    let s: f32 = exps.iter().sum();
    exps.iter().map(|&e| e / s).collect()
}

fn argmax(row: &[f32]) -> usize {
    let mut best = 0;
    for i in 1..row.len() {
        if row[i] > row[best] {
            best = i;
        }
    }
    best
}

// ─── accent model (char-level put_accent) ────────────────────────────────────────────────────────

pub struct AccentModel {
    session: Session,
    id2label: Vec<String>,
    tok: CharTokenizer,
}

impl AccentModel {
    pub fn load(dir: &Path) -> io::Result<Self> {
        Ok(AccentModel {
            session: load_session(&dir.join("model.onnx"))?,
            id2label: load_id2label(dir)?,
            tok: CharTokenizer::load(dir)?,
        })
    }

    /// accent_model.put_accent — char-level stress. Runs the lowercased word through the ONNX model,
    /// then marks each char whose per-position label is a real stress (not NO / STRESS_SECONDARY) with
    /// score ≥ 0.55, inserting `+` BEFORE that char. The `+` is applied to the ORIGINAL-case word
    /// (same char count as the lowercased form), faithfully to render_stress's `text[i-1]` indexing
    /// (i counts bos/eos too; Python's `text[-1]` wrap for i==0 is preserved).
    pub fn put_accent(&mut self, word: &str) -> io::Result<String> {
        let lower = word.to_lowercase();
        let enc = self.tok.encode(&lower);
        let seq = enc.input_ids.len();
        let (shape, logits) = run_logits(&mut self.session, &enc.input_ids, &enc.attention_mask, &enc.token_type_ids, 1, seq)?;
        let classes = *shape.last().unwrap_or(&0) as usize;

        let mut text: Vec<String> = word.chars().map(|c| c.to_string()).collect();
        if text.is_empty() {
            return Ok(String::new());
        }
        for i in 0..seq {
            let row = &logits[i * classes..(i + 1) * classes];
            let probs = softmax(row);
            let label_id = argmax(row); // argmax over logits == over softmax
            let score = probs[label_id];
            let label = self.id2label.get(label_id).map(String::as_str).unwrap_or("");
            if label != "NO" && label != "STRESS_SECONDARY" && score >= 0.55 {
                let idx = if i == 0 { text.len() - 1 } else { i - 1 };
                if idx < text.len() {
                    text[idx] = format!("+{}", text[idx]);
                }
            }
        }
        Ok(text.concat())
    }
}

// ─── BERT NER models (stress_usage, yo_homograph) ────────────────────────────────────────────────

/// Token-classification model returning one label per word. `stress_usage` gates `_process_accent`
/// (label "STRESS"); `yo_homograph` gates the `yo_homographs` dict lookup (label "YO"). Both share
/// the exact same decode (ner::decode); only the model dir / labels / input text differ.
pub struct BertNerModel {
    session: Session,
    id2label: Vec<String>,
    tok: BertTokenizer,
}

impl BertNerModel {
    pub fn load(dir: &Path) -> io::Result<Self> {
        Ok(BertNerModel {
            session: load_session(&dir.join("model.onnx"))?,
            id2label: load_id2label(dir)?,
            tok: BertTokenizer::load(dir)?,
        })
    }

    /// predict_stress_usage / predict_yo_homographs → extract_entities: the per-word label list.
    pub fn predict(&mut self, text: &str) -> io::Result<Vec<String>> {
        let enc = self.tok.encode(text)?;
        let seq = enc.input_ids.len();
        let (shape, logits) =
            run_logits(&mut self.session, &enc.input_ids, &enc.attention_mask, &enc.token_type_ids, 1, seq)?;
        let classes = *shape.last().unwrap_or(&0) as usize;
        let scores: Vec<Vec<f32>> =
            (0..seq).map(|i| softmax(&logits[i * classes..(i + 1) * classes])).collect();
        Ok(ner::decode(text, &enc, &scores, self.tok.unk_id(), &self.id2label))
    }
}

// ─── omograph model (sequence-pair classifier) ───────────────────────────────────────────────────

// omograph_model.py special_words: bases whose >3-member groups split into 3s (not 2s) in group_words.
const SPECIAL_WORDS: &[&str] = &[
    "балчуга", "вертела", "волоки", "волоку", "воронью", "выбродите", "вывозите", "выносите",
    "выноситесь", "выходите", "железы", "начала", "округа", "перепела", "развитая", "развитого",
    "развитое", "развитой", "развитом", "развитому", "развитою", "развитую", "развитые", "развитым",
    "развитыми", "развитых", "сторожа", "сторожи", "сторожу", "удало", "начался", "началась",
    "началось", "бутиках", "ожила", "создало", "коротки", "проклята", "роженица", "роженицы",
    "рожениц", "роженице", "роженицам", "роженицу", "роженицей", "роженицею", "роженицами",
    "роженицах", "пристава", "приставов", "приставам", "приставами", "приставах", "пережитое",
    "пережитого", "пережитые", "пережитых", "пережитому", "пережитым", "пережитыми", "пережитом",
    "нипоняла",
];

/// omograph_model.classify's `re.sub(r'\s+(?=[,.?!:;…])', '', text)` — drop each whitespace run that
/// is immediately followed by one of these punctuation marks. (The `regex` crate has no lookahead, so
/// this is done by hand; the result is identical.)
fn strip_ws_before_punct(text: &str) -> String {
    const FOLLOW: &str = ",.?!:;…";
    let chars: Vec<char> = text.chars().collect();
    let mut out = String::with_capacity(text.len());
    let mut i = 0;
    while i < chars.len() {
        if chars[i].is_whitespace() {
            let start = i;
            while i < chars.len() && chars[i].is_whitespace() {
                i += 1;
            }
            let followed_by_punct = i < chars.len() && FOLLOW.contains(chars[i]);
            if !followed_by_punct {
                out.extend(&chars[start..i]);
            }
        } else {
            out.push(chars[i]);
            i += 1;
        }
    }
    out
}

/// omograph_model.group_words — split the flat hypothesis list into per-word groups (same base word =
/// same variant set), further subdividing oversized groups (special-word bases into 3s; other even
/// >3 groups into 2s).
fn group_words(words: &[String]) -> Vec<Vec<String>> {
    if words.is_empty() {
        return Vec::new();
    }
    let base = |w: &str| w.replace('+', "");
    let mut result: Vec<Vec<String>> = Vec::new();
    let flush = |result: &mut Vec<Vec<String>>, group: Vec<String>, cur_base: &str| {
        if SPECIAL_WORDS.contains(&cur_base) && group.len() > 3 {
            for chunk in group.chunks(3) {
                result.push(chunk.to_vec());
            }
        } else if group.len() > 3 && group.len() % 2 == 0 {
            for chunk in group.chunks(2) {
                result.push(chunk.to_vec());
            }
        } else {
            result.push(group);
        }
    };
    let mut current_group = vec![words[0].clone()];
    let mut current_base = base(&words[0]);
    for word in &words[1..] {
        let b = base(word);
        if b == current_base {
            current_group.push(word.clone());
        } else {
            flush(&mut result, std::mem::take(&mut current_group), &current_base);
            current_group.push(word.clone());
            current_base = b;
        }
    }
    flush(&mut result, current_group, &current_base);
    result
}

/// omograph_model.transfer_grouping — split `target` into slices whose lengths mirror `grouped`.
fn transfer_grouping<T: Clone>(grouped: &[Vec<String>], target: &[T]) -> Vec<Vec<T>> {
    let mut out = Vec::with_capacity(grouped.len());
    let mut start = 0usize;
    for g in grouped {
        let end = (start + g.len()).min(target.len());
        out.push(target[start..end].to_vec());
        start += g.len();
    }
    out
}

pub struct OmographModel {
    session: Session,
    tok: BertTokenizer,
}

impl OmographModel {
    pub fn load(dir: &Path) -> io::Result<Self> {
        Ok(OmographModel { session: load_session(&dir.join("model.onnx"))?, tok: BertTokenizer::load(dir)? })
    }

    fn classify_pair(&mut self, text: &str, hyp: &str) -> io::Result<f32> {
        let enc = self.tok.encode_pair(text, hyp)?;
        let seq = enc.input_ids.len();
        let ttype = vec![0i64; seq];
        let (_, logits) = run_logits(&mut self.session, &enc.input_ids, &enc.attention_mask, &ttype, 1, seq)?;
        // self.softmax over the single [2] row → normalized; prob of the "true" class (index 1).
        Ok(softmax(&logits)[1])
    }

    /// omograph_model.classify. `num_hypotheses[k]` = variant count of homograph k (in order); when all
    /// are even it takes the batched pair-classification path, otherwise the per-hypothesis NO_BATCH
    /// path (with group_words re-splitting). Returns the chosen stressed variant per homograph.
    pub fn classify(
        &mut self,
        texts: &[String],
        hypotheses: &[String],
        num_hypotheses: &[usize],
    ) -> io::Result<Vec<String>> {
        let preprocessed: Vec<String> = texts.iter().map(|t| strip_ws_before_punct(t)).collect();

        if !num_hypotheses.iter().all(|&n| n % 2 == 0) {
            // NO_BATCH: group hypotheses by base word; each group classified variant-by-variant.
            let grouped_h = group_words(hypotheses);
            let grouped_t = transfer_grouping(&grouped_h, &preprocessed);
            let mut outs = Vec::with_capacity(grouped_h.len());
            for (h, t) in grouped_h.iter().zip(grouped_t.iter()) {
                let base_text = t.first().cloned().unwrap_or_default();
                let mut probs = Vec::with_capacity(h.len());
                for hp in h {
                    probs.push(self.classify_pair(&base_text, hp)?);
                }
                let best = probs.iter().enumerate().fold(0usize, |b, (i, &p)| if p > probs[b] { i } else { b });
                outs.push(h[best].clone());
            }
            return Ok(outs);
        }

        // Batched: encode all (text, hyp) pairs, pad to the batch-longest, one ONNX call → [N, 2].
        let n = preprocessed.len();
        let mut encs = Vec::with_capacity(n);
        let mut maxlen = 0usize;
        for i in 0..n {
            let mut e = self.tok.encode_pair(&preprocessed[i], &hypotheses[i])?;
            if e.input_ids.len() > 512 {
                // truncation=True, max_length=512 (rare for these short strings).
                e.input_ids.truncate(512);
                e.attention_mask.truncate(512);
            }
            maxlen = maxlen.max(e.input_ids.len());
            encs.push(e);
        }
        let pad = self.tok.pad_id();
        let mut ids = vec![pad; n * maxlen];
        let mut attn = vec![0i64; n * maxlen];
        for (r, e) in encs.iter().enumerate() {
            for (c, (&id, &a)) in e.input_ids.iter().zip(e.attention_mask.iter()).enumerate() {
                ids[r * maxlen + c] = id;
                attn[r * maxlen + c] = a;
            }
        }
        let ttype = vec![0i64; n * maxlen];
        let (shape, logits) = run_logits(&mut self.session, &ids, &attn, &ttype, n, maxlen)?;
        let classes = *shape.last().unwrap_or(&2) as usize; // 2

        // self.softmax is a GLOBAL softmax over the whole [N, classes] array; we then read column 1.
        // (Global normalization is monotonic, so it preserves the per-pair argmax — replicated for
        // faithfulness.)
        let sm = global_softmax(&logits);
        let col1: Vec<f32> = (0..n).map(|i| sm[i * classes + 1]).collect();

        // Pair up consecutive rows (Python assumes exactly 2 variants per homograph) and pick the
        // higher-prob variant; ties take the first (Python `list.index(max)`).
        let mut outs = Vec::with_capacity(n / 2);
        for k in 0..n / 2 {
            let (p0, p1) = (col1[2 * k], col1[2 * k + 1]);
            let idx = if p1 > p0 { 1 } else { 0 };
            outs.push(hypotheses[2 * k + idx].clone());
        }
        Ok(outs)
    }
}

/// Global softmax over a whole flat array (numpy `exp(x-max)/sum` with scalar max/sum) — matches
/// omograph_model.softmax applied to the batched `[N, 2]` output.
fn global_softmax(x: &[f32]) -> Vec<f32> {
    let m = x.iter().cloned().fold(f32::NEG_INFINITY, f32::max);
    let exps: Vec<f32> = x.iter().map(|&v| (v - m).exp()).collect();
    let s: f32 = exps.iter().sum();
    exps.iter().map(|&e| e / s).collect()
}
