//! Thin wrapper over the HuggingFace `tokenizers` crate (same lib the Python models use) for the
//! BERT WordPiece tokenizers of the stress_usage / yo_homograph NER models, and the byte-level BPE
//! tokenizer of the omograph pair-classifier. Loading `tokenizer.json` directly guarantees identical
//! ids/offsets/special-mask to Python `AutoTokenizer.from_pretrained`.

use std::io;
use std::path::Path;

use tokenizers::Tokenizer;

pub struct BertTokenizer {
    tok: Tokenizer,
    unk_id: u32,
    pad_id: i64,
}

/// One encoded sequence, mirroring the fields the Python NER pipeline pulls from the fast tokenizer.
pub struct Encoding {
    pub input_ids: Vec<i64>,
    pub attention_mask: Vec<i64>,
    pub token_type_ids: Vec<i64>,
    pub special_tokens_mask: Vec<i64>,
    pub tokens: Vec<String>,
    /// Byte offsets into the original string (Rust tokenizers are byte-based). We slice bytes with
    /// these, so `word_ref` text — and its char length — matches Python's char-offset slicing exactly.
    pub offsets: Vec<(usize, usize)>,
}

impl BertTokenizer {
    pub fn load(dir: &Path) -> io::Result<Self> {
        let tok = Tokenizer::from_file(dir.join("tokenizer.json"))
            .map_err(|e| io::Error::other(format!("load tokenizer.json: {e}")))?;
        // [UNK] (BERT) or <unk> (RoBERTa/BPE omograph); [PAD]/<pad> for batch padding.
        let unk_id = tok
            .token_to_id("[UNK]")
            .or_else(|| tok.token_to_id("<unk>"))
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "tokenizer has no unk token"))?;
        let pad_id = tok.token_to_id("[PAD]").or_else(|| tok.token_to_id("<pad>")).unwrap_or(0) as i64;
        Ok(BertTokenizer { tok, unk_id, pad_id })
    }

    pub fn unk_id(&self) -> i64 {
        self.unk_id as i64
    }

    pub fn pad_id(&self) -> i64 {
        self.pad_id
    }

    /// Encode a sequence pair with special tokens (`tokenizer(a, b, ...)`), for the omograph
    /// pair-classifier. The tokenizer.json's post-processor (RobertaProcessing) inserts the
    /// `<s>…</s></s>…</s>` markers. Only `input_ids`/`attention_mask` are needed downstream.
    pub fn encode_pair(&self, a: &str, b: &str) -> io::Result<Encoding> {
        let e = self
            .tok
            .encode((a, b), true)
            .map_err(|e| io::Error::other(format!("encode pair: {e}")))?;
        Ok(Encoding {
            input_ids: e.get_ids().iter().map(|&x| x as i64).collect(),
            attention_mask: e.get_attention_mask().iter().map(|&x| x as i64).collect(),
            token_type_ids: e.get_type_ids().iter().map(|&x| x as i64).collect(),
            special_tokens_mask: e.get_special_tokens_mask().iter().map(|&x| x as i64).collect(),
            tokens: e.get_tokens().to_vec(),
            offsets: e.get_offsets().to_vec(),
        })
    }

    /// Encode a single sequence with special tokens (`tokenizer(text, ...)` in Python).
    pub fn encode(&self, text: &str) -> io::Result<Encoding> {
        let e = self
            .tok
            .encode(text, true)
            .map_err(|e| io::Error::other(format!("encode: {e}")))?;
        Ok(Encoding {
            input_ids: e.get_ids().iter().map(|&x| x as i64).collect(),
            attention_mask: e.get_attention_mask().iter().map(|&x| x as i64).collect(),
            token_type_ids: e.get_type_ids().iter().map(|&x| x as i64).collect(),
            special_tokens_mask: e.get_special_tokens_mask().iter().map(|&x| x as i64).collect(),
            tokens: e.get_tokens().to_vec(),
            offsets: e.get_offsets().to_vec(),
        })
    }
}
