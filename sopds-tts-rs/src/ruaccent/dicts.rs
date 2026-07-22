//! RUAccent dictionaries (`$RUACCENT_HOME/dictionary/*.json.gz`), loaded into HashMaps.
//! Ported from ruaccent.py `load()` (lines 89-116).

use flate2::read::GzDecoder;
use std::collections::HashMap;
use std::fs::File;
use std::io::{self, Read};
use std::path::Path;

pub struct Dicts {
    /// word -> stressed form (with `+`). `accents.json.gz` (dictionary) or `accents_nn.json.gz`.
    pub accents: HashMap<String, String>,
    /// word -> stressed variants for disambiguation by the omograph model.
    pub omographs: HashMap<String, Vec<String>>,
    /// word -> ё-restored form (unambiguous).
    pub yo_words: HashMap<String, String>,
    /// word -> ё-restored form (used only when the yo model flags the word `YO`).
    pub yo_homographs: HashMap<String, String>,
}

impl Dicts {
    /// `use_dictionary=true` loads the full `accents.json.gz`; false loads the neural `accents_nn`.
    pub fn load(home: &Path, use_dictionary: bool) -> io::Result<Self> {
        let d = home.join("dictionary");

        let mut omographs: HashMap<String, Vec<String>> = load_gz_json(&d.join("omographs.json.gz"))?;
        // ruaccent.py:92 — a hardcoded extra entry.
        omographs.insert("коса".to_string(), vec!["к+оса".to_string(), "кос+а".to_string()]);

        let yo_words = load_gz_json(&d.join("yo_words.json.gz"))?;
        let yo_homographs = load_gz_json(&d.join("yo_homographs.json.gz"))?;

        let accents_file = if use_dictionary { "accents.json.gz" } else { "accents_nn.json.gz" };
        let mut accents: HashMap<String, String> = load_gz_json(&d.join(accents_file))?;
        // ruaccent.py `letters_accent`, applied last (ruaccent.py:116).
        accents.insert("о".to_string(), "+о".to_string());
        accents.insert("О".to_string(), "+О".to_string());

        Ok(Dicts { accents, omographs, yo_words, yo_homographs })
    }
}

fn load_gz_json<T: serde::de::DeserializeOwned>(path: &Path) -> io::Result<T> {
    let file = File::open(path).map_err(|e| io::Error::new(e.kind(), format!("{}: {e}", path.display())))?;
    let mut s = String::new();
    GzDecoder::new(file).read_to_string(&mut s)?;
    serde_json::from_str(&s).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, format!("{}: {e}", path.display())))
}
