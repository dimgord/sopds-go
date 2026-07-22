//! CharTokenizer for the accent model — port of ruaccent/char_tokenizer.py.
//! Each character is a token; ids come from `vocab.txt` (line N → id N). Encoding wraps the char ids
//! with `[bos] … [eos]` (build_inputs_with_special_tokens). do_lower_case is False in RUAccent (the
//! caller lowercases first), so we tokenize the string as-is.

use std::collections::HashMap;
use std::io;
use std::path::Path;

pub struct CharTokenizer {
    vocab: HashMap<char, i64>,
    unk_id: i64,
    bos_id: i64,
    eos_id: i64,
}

/// One encoded sequence: `input_ids` already wrapped with bos/eos; masks are all-ones / all-zeros as
/// in the HF fast-tokenizer output for a single sequence.
pub struct Encoded {
    pub input_ids: Vec<i64>,
    pub attention_mask: Vec<i64>,
    pub token_type_ids: Vec<i64>,
}

impl CharTokenizer {
    pub fn load(dir: &Path) -> io::Result<Self> {
        let text = std::fs::read_to_string(dir.join("vocab.txt"))?;
        // vocab.txt is one token per line; the token is a single char (or a `[bos]`/`[unk]` marker).
        // `.lines()` strips `\n` and a trailing `\r` (CRLF-safe), matching char_tokenizer.load_vocab's
        // `rstrip("\n")` on this ASCII-keyed file.
        let mut vocab: HashMap<char, i64> = HashMap::new();
        let mut specials: HashMap<String, i64> = HashMap::new();
        for (i, line) in text.lines().enumerate() {
            let id = i as i64;
            specials.insert(line.to_string(), id);
            let mut chars = line.chars();
            if let Some(c) = chars.next() {
                if chars.next().is_none() {
                    vocab.insert(c, id); // single-char token
                }
            }
        }
        let get = |k: &str| -> io::Result<i64> {
            specials
                .get(k)
                .copied()
                .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, format!("vocab.txt missing {k}")))
        };
        Ok(CharTokenizer {
            unk_id: get("[unk]")?,
            bos_id: get("[bos]")?,
            eos_id: get("[eos]")?,
            vocab,
        })
    }

    pub fn unk_id(&self) -> i64 {
        self.unk_id
    }

    /// `tokenizer(text)` — chars → ids, wrapped bos…eos. `text` is expected already-lowercased.
    pub fn encode(&self, text: &str) -> Encoded {
        let mut ids = Vec::with_capacity(text.chars().count() + 2);
        ids.push(self.bos_id);
        for c in text.chars() {
            ids.push(*self.vocab.get(&c).unwrap_or(&self.unk_id));
        }
        ids.push(self.eos_id);
        let n = ids.len();
        Encoded { input_ids: ids, attention_mask: vec![1; n], token_type_ids: vec![0; n] }
    }
}
