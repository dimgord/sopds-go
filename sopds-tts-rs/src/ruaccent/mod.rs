//! Native Rust port of RUAccent (Russian stress placement) — replaces the Python `ruaccent_batch.py`
//! / `$RUPY` in the auto-F5 pipeline. See docs/decisions/004-ruaccent-rust-port.md.
//!
//! Phase 1 (this file): dictionaries + text preprocessing + the *dictionary* path of stress
//! placement (`tiny_mode`-equivalent minus the neural fallback). The four ONNX models
//! (stress_usage, yo_homograph, omograph, accent) and a faithful razdel sentence splitter land in
//! Phases 2-3. Parity target is bit-exact vs Python (ort/tokenizers are the same underlying libs).

#![allow(dead_code)] // wired into a `stress` subcommand + neural models in later phases

mod dicts;
mod preprocess;

use dicts::Dicts;
use std::io;
use std::path::Path;

pub struct RuAccent {
    dicts: Dicts,
}

impl RuAccent {
    /// Load from `$RUACCENT_HOME` (the dir holding `dictionary/` and `nn/`). Phase 1 loads the
    /// full accent dictionary; Phase 2 also opens the ONNX sessions here.
    pub fn load(home: &Path) -> io::Result<Self> {
        Ok(RuAccent { dicts: Dicts::load(home, true)? })
    }

    /// Stress `text`. Phase 1: dictionary-only — known words get their dict stress, OOV words are
    /// left unstressed (the neural accent model fills those in Phase 2). Sentence splitting is
    /// still naive here (whole input as one unit); razdel parity comes in Phase 3.
    pub fn process_all(&self, text: &str) -> String {
        let text = preprocess::normalize(text);
        // TODO Phase 3: preprocess::split_by_sentences (razdel). One unit is fine for the dict path.
        let (words, remaining) = preprocess::split_by_words(&text);
        if words.is_empty() {
            return remaining.concat();
        }
        let processed = self.process_accent_dict(words);
        let mut out = String::new();
        for (l, r) in remaining.iter().zip(processed.iter()) {
            out.push_str(l);
            out.push_str(r);
        }
        out.push_str(remaining.last().expect("split_by_words yields >=1 remaining when words nonempty"));
        preprocess::delete_spaces_before_punc(&out)
    }

    #[cfg(test)]
    pub(crate) fn dict_len(&self) -> usize {
        self.dicts.accents.len()
    }

    /// ruaccent.py `_process_accent`, dictionary branch only (Phase 1). Phase 2 adds the neural
    /// `accent_model.put_accent` for in-vocab-miss multi-vowel words, and the stress_usage gate.
    fn process_accent_dict(&self, mut words: Vec<String>) -> Vec<String> {
        for w in words.iter_mut() {
            if w.contains('+') {
                continue;
            }
            let lower = w.to_lowercase();
            match self.dicts.accents.get(&lower) {
                // dict hit (stressed form differs from the bare lowercase) → carry the `+` onto the
                // original-case word.
                Some(stressed) if *stressed != lower => *w = preprocess::transfer_plus(w, stressed),
                // dict miss → Phase 2 neural accent model; Phase 1 leaves it unstressed.
                _ => {}
            }
        }
        words
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Gated on the real dictionary (~/.cache/ruaccent). Skips silently if absent (CI). Validates the
    // Phase-1 dict-only stress path: dictionary hits stress correctly with case preserved, OOV words
    // pass through untouched (neural fallback is Phase 2).
    #[test]
    fn dict_only_stress() {
        let home = match std::env::var_os("RUACCENT_HOME") {
            Some(h) => std::path::PathBuf::from(h),
            None => dirs_cache(),
        };
        if !home.join("dictionary/accents.json.gz").exists() {
            eprintln!("skip dict_only_stress: no dict at {}", home.display());
            return;
        }
        let ra = RuAccent::load(&home).expect("load dicts");
        assert!(ra.dict_len() > 1_000_000, "accents dict looks too small: {}", ra.dict_len());
        // A dictionary word gets its stored stress; the dict is the source of truth (avoids pinning a
        // specific position for a homograph like "замок").
        let stressed = ra.dicts.accents.get("замок").expect("замок in dict").clone();
        assert!(stressed.contains('+'), "dict entry has no stress: {stressed:?}");
        assert_eq!(ra.process_all("замок"), stressed);
        // Capital form: same stress position, uppercase first letter preserved.
        let cap = ra.process_all("Замок");
        assert!(cap.starts_with('З') && cap.contains('+'), "capital not preserved: {cap}");
        // Whole sentence: punctuation/spacing intact, at least one word stressed.
        let out = ra.process_all("Старый замок.");
        assert!(out.contains('+') && out.ends_with('.'), "unexpected: {out}");
    }

    fn dirs_cache() -> std::path::PathBuf {
        let home = std::env::var_os("HOME").map(std::path::PathBuf::from).unwrap_or_default();
        home.join(".cache/ruaccent")
    }
}
