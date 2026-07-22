//! Native Rust port of RUAccent (Russian stress placement) — replaces the Python `ruaccent_batch.py`
//! / `$RUPY` in the auto-F5 pipeline. See docs/decisions/004-ruaccent-rust-port.md.
//!
//! Phase 2 (this file): full stress pipeline — dictionaries + preprocessing + all four ONNX models
//! (stress_usage, yo_homograph, omograph, accent), orchestrated exactly as ruaccent.py
//! `process_all_internal`. Sentence splitting is still naive (whole input = one sentence); the
//! faithful razdel port lands in Phase 3. Parity target is bit-exact vs Python (ort/tokenizers are
//! the same underlying libs).

#![allow(dead_code)] // wired into a `stress` subcommand in Phase 4

mod char_tok;
mod dicts;
mod models;
mod ner;
mod preprocess;
mod razdel;
mod tok_bert;

use dicts::Dicts;
use models::{AccentModel, BertNerModel, OmographModel};
use std::io;
use std::path::Path;

pub struct RuAccent {
    dicts: Dicts,
    stress: BertNerModel,
    yo: BertNerModel,
    omograph: OmographModel,
    accent: AccentModel,
}

impl RuAccent {
    /// Load from `$RUACCENT_HOME` (the dir holding `dictionary/` and `nn/`): the full accent
    /// dictionary plus the four ONNX models (turbo2 omograph, as in RUAccent's default `load()`).
    pub fn load(home: &Path) -> io::Result<Self> {
        let nn = home.join("nn");
        Ok(RuAccent {
            dicts: Dicts::load(home, true)?, // use_dictionary=true (the big accents.json.gz)
            stress: BertNerModel::load(&nn.join("nn_stress_usage_predictor"))?,
            yo: BertNerModel::load(&nn.join("nn_yo_homograph_resolver"))?,
            omograph: OmographModel::load(&nn.join("nn_omograph/turbo2"))?,
            accent: AccentModel::load(&nn.join("nn_accent"))?,
        })
    }

    /// Sorted union of the ambiguous homograph words (`yo_homographs` ∪ `omographs` keys) — the
    /// `--dump-homographs` output of `ruaccent_batch.py`, used to flag ё-restorations worth review.
    pub fn homograph_words(&self) -> Vec<String> {
        let mut set: std::collections::BTreeSet<&String> = std::collections::BTreeSet::new();
        set.extend(self.dicts.yo_homographs.keys());
        set.extend(self.dicts.omographs.keys());
        set.into_iter().cloned().collect()
    }

    /// Override a `yo_homographs` entry (the `--fix` file's `"yo"` map in `ruaccent_batch.py`).
    pub fn set_yo_override(&mut self, word: String, form: String) {
        self.dicts.yo_homographs.insert(word, form);
    }

    /// ruaccent.py `process_all_internal`: normalize → split into sentences → per sentence run the
    /// stress_usage NER, yo restoration, omograph disambiguation, and accent placement, then rejoin.
    pub fn process_all(&mut self, text: &str) -> io::Result<String> {
        let text = preprocess::normalize(text);
        let mut out = String::new();
        for sentence in razdel::split_by_sentences(&text) {
            let (words, remaining) = preprocess::split_by_words(&sentence);
            if words.is_empty() {
                out.push_str(&remaining.concat());
                continue;
            }
            let stress_usages = self.stress.predict(&sentence)?;
            let mut words = self.process_yo(words, &sentence)?;
            words = self.process_omographs(words)?;
            words = self.process_accent(words, &stress_usages)?;
            // "".join([l+r for l,r in zip(remaining_text, words)] + [remaining_text[-1]])
            let mut s = String::new();
            for (l, r) in remaining.iter().zip(words.iter()) {
                s.push_str(l);
                s.push_str(r);
            }
            s.push_str(remaining.last().expect(">=1 remaining when words nonempty"));
            out.push_str(&preprocess::delete_spaces_before_punc(&s));
        }
        Ok(out)
    }

    /// ruaccent.py `_process_yo`: dict `yo_words` restores unambiguous ё; when the yo NER flags a word
    /// `YO`, the ambiguous `yo_homographs` dict applies. Case carried via `fix_capital`.
    fn process_yo(&mut self, mut words: Vec<String>, sentence: &str) -> io::Result<Vec<String>> {
        let lower_sentence = sentence.to_lowercase();
        let yo_predictions = if lower_sentence.contains('е') {
            Some(self.yo.predict(&lower_sentence)?)
        } else {
            None
        };
        for i in 0..words.len() {
            let word = words[i].clone();
            let lower_word = word.to_lowercase();
            let yo_w = self.dicts.yo_words.get(&lower_word).cloned().unwrap_or_else(|| word.clone());
            words[i] = preprocess::fix_capital(&word, &yo_w);
            if yo_predictions.as_ref().and_then(|p| p.get(i)).map(|e| e == "YO").unwrap_or(false) {
                let yh = self.dicts.yo_homographs.get(&lower_word).cloned().unwrap_or_else(|| word.clone());
                words[i] = preprocess::fix_capital(&word, &yh);
            }
        }
        Ok(words)
    }

    /// ruaccent.py `_process_omographs`: for each word in the `omographs` dict, wrap it `<w>…</w>` in a
    /// space-joined copy of the sentence and let the omograph model pick the correct stressed variant.
    fn process_omographs(&mut self, mut words: Vec<String>) -> io::Result<Vec<String>> {
        let founded: Vec<(Vec<String>, usize)> = words
            .iter()
            .enumerate()
            .filter_map(|(i, w)| self.dicts.omographs.get(w).map(|v| (v.clone(), i)))
            .collect();
        if founded.is_empty() {
            return Ok(words);
        }
        let hypotheses: Vec<String> = founded.iter().flat_map(|(v, _)| v.clone()).collect();
        let num_hypotheses: Vec<usize> = founded.iter().map(|(v, _)| v.len()).collect();
        let mut texts_batch: Vec<String> = Vec::new();
        for (variants, position) in &founded {
            let t_back = words[*position].clone();
            words[*position] = format!(" <w>{t_back}</w> ");
            for _ in 0..variants.len() {
                texts_batch.push(preprocess::delete_spaces_before_punc(&words.join(" ")));
            }
            words[*position] = t_back;
        }
        let cls = self.omograph.classify(&texts_batch, &hypotheses, &num_hypotheses)?;
        for (cls_index, (_, position)) in founded.iter().enumerate() {
            if let Some(c) = cls.get(cls_index) {
                words[*position] = c.clone();
            }
        }
        Ok(words)
    }

    /// ruaccent.py `_process_accent`: for each still-unstressed word the NER marks `STRESS`, apply the
    /// dict stress (`transfer_plus`); on a dict miss with >1 vowel and no punctuation, the neural
    /// accent model places it.
    fn process_accent(&mut self, mut words: Vec<String>, stress_usages: &[String]) -> io::Result<Vec<String>> {
        for i in 0..words.len() {
            if words[i].contains('+') {
                continue;
            }
            if stress_usages.get(i).map(|s| s == "STRESS").unwrap_or(false) {
                let word = words[i].clone();
                let lower_word = word.to_lowercase();
                let stressed = self.dicts.accents.get(&lower_word).cloned().unwrap_or_else(|| lower_word.clone());
                if stressed == lower_word
                    && !preprocess::has_punctuation(&lower_word)
                    && preprocess::count_vowels(&lower_word) > 1
                {
                    words[i] = self.accent.put_accent(&word)?;
                } else {
                    words[i] = preprocess::transfer_plus(&word, &stressed);
                }
            }
        }
        Ok(words)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Faithful razdel `split_by_sentences` — bit-exact vs Python `TextPreprocessor.split_by_sentences`
    // (abbreviations, initials, paired sokr, quotes, bullet lists, no-final-punct). No models needed.
    #[test]
    fn sentence_split_parity() {
        let cases: &[(&str, &[&str])] = &[
            ("Привет. Как дела?", &["Привет.", " Как дела?"]),
            ("Он пришёл домой. Было поздно.", &["Он пришёл домой.", " Было поздно."]),
            ("Это стоит 5 руб. и ни копейки больше.", &["Это стоит 5 руб. и ни копейки больше."]),
            ("А. С. Пушкин родился в Москве.", &["А. С. Пушкин родился в Москве."]),
            ("Первый пункт. Второй пункт! Третий?", &["Первый пункт.", " Второй пункт!", " Третий?"]),
            ("Она сказала: «Иди сюда». Он пошёл.", &["Она сказала: «Иди сюда».", " Он пошёл."]),
            ("Список: 1. хлеб 2. молоко.", &["Список: 1. хлеб 2. молоко."]),
            ("В 1999 г. случилось это. А потом всё изменилось.", &["В 1999 г. случилось это.", " А потом всё изменилось."]),
            ("Т. е. это конец. Начало другого.", &["Т. е. это конец.", " Начало другого."]),
            ("Одно предложение без конца", &["Одно предложение без конца"]),
            // Edge cases: ellipsis, ?!, smiley, brackets, roman-numeral chapters, dash, newline gap.
            ("Он ушёл... Она осталась.", &["Он ушёл...", " Она осталась."]),
            ("Что?! Неужели?", &["Что?!", " Неужели?"]),
            ("Смотри :) это смайлик. Да.", &["Смотри :) это смайлик.", " Да."]),
            ("Дом (большой) стоял тут. Рядом лес.", &["Дом (большой) стоял тут.", " Рядом лес."]),
            ("Гл. I. Начало. Гл. II. Конец.", &["Гл. I. Начало.", " Гл. II.", " Конец."]),
            ("Текст — это хорошо. Ещё текст.", &["Текст — это хорошо.", " Ещё текст."]),
            ("Первая строка.\nВторая строка.", &["Первая строка.", "\nВторая строка."]),
        ];
        for (input, want) in cases {
            assert_eq!(razdel::split_by_sentences(input), *want, "split({input:?})");
        }
    }

    // Full-pipeline parity vs Python `RUAccent.process_all` (turbo2, use_dictionary=True), on
    // single-sentence inputs (naive sentence split matches razdel there). Gated on the model dir.
    #[test]
    fn process_all_parity() {
        let home = match std::env::var_os("RUACCENT_HOME") {
            Some(h) => std::path::PathBuf::from(h),
            None => dirs_cache(),
        };
        if !home.join("nn/nn_accent/model.onnx").exists() {
            eprintln!("skip process_all_parity: models absent at {}", home.display());
            return;
        }
        let mut ra = RuAccent::load(&home).expect("load RUAccent");
        let cases = [
            ("Старый замок стоял на горе.", "Ст+арый з+амок сто+ял на гор+е."),
            ("Он запер замок на ключ.", "Он з+апер зам+ок на кл+юч."),
            ("Мужики косили траву косой.", "Мужик+и кос+или трав+у кос+ой."),
            ("Дорога шла через село.", "Дор+ога шла ч+ерез сел+о."),
            ("Я иду домой, а ты идёшь в магазин.", "Я ид+у дом+ой, а т+ы идёшь в магаз+ин."),
            ("На двери висел замок.", "На двер+и вис+ел зам+ок."),
            // NB: turbo2 mis-stresses "Белки" (proteins) as "Б+елки" (squirrels) — a KNOWN model
            // limitation, not a port bug (the omographs dict has both б+елки / белк+и, but the model
            // always leans to б+елки when the word is capitalised sentence-initial). Kept as a *parity*
            // check (Rust must reproduce Python exactly), NOT as a claim of correct linguistics.
            ("Белки в организме.", "Б+елки в орган+изме."),
            ("Хлопковое поле.", "Хл+опковое п+оле."),
            // ё restoration (yo_words dict) + capital carry.
            ("Ёжик нёс ёлку через ручей.", "Ёжик нёс ёлку ч+ерез руч+ей."),
            ("Все пришли на день рождения.", "Вс+е пришл+и на д+ень рожд+ения."),
            ("Учёный записал результаты в тетрадь.", "Учёный запис+ал результ+аты в тетр+адь."),
            ("Она не могла найти свои ключи.", "Он+а н+е могл+а найт+и сво+и ключ+и."),
            (
                "Дети играли во дворе целый день, а вечером пили чай с вареньем.",
                "Д+ети игр+али в+о двор+е ц+елый д+ень, а в+ечером п+или ч+ай с вар+еньем.",
            ),
            // Same homograph twice, different reading by position: З+амок (castle) … зам+ок (lock).
            ("Замок был заперт на большой замок.", "З+амок был з+аперт н+а больш+ой зам+ок."),
            ("Три мушкетёра и один гвардеец.", "Тр+и мушкетёра и од+ин гвард+еец."),
            // Multi-sentence (exercises the razdel split feeding per-sentence NER).
            (
                "Старый замок стоял на горе. Он запер замок на ключ.",
                "Ст+арый з+амок сто+ял на гор+е. Он з+апер зам+ок на кл+юч.",
            ),
            ("Привет. Как дела? Всё хорошо.", "Прив+ет. К+ак дел+а? Вс+ё хорош+о."),
            (
                "Она сказала: «Иди сюда». Он пошёл домой.",
                "Он+а сказ+ала: «Ид+и сюд+а». +Он пошёл дом+ой.",
            ),
        ];
        for (input, want) in cases {
            assert_eq!(ra.process_all(input).unwrap(), want, "process_all({input:?})");
        }
    }

    fn dirs_cache() -> std::path::PathBuf {
        let home = std::env::var_os("HOME").map(std::path::PathBuf::from).unwrap_or_default();
        home.join(".cache/ruaccent")
    }

    // Bit-exact vs Python `AccentModel.put_accent` (char-level ONNX). Gated on the model dir.
    #[test]
    fn accent_model_put_accent() {
        let home = match std::env::var_os("RUACCENT_HOME") {
            Some(h) => std::path::PathBuf::from(h),
            None => dirs_cache(),
        };
        let dir = home.join("nn/nn_accent");
        if !dir.join("model.onnx").exists() {
            eprintln!("skip accent_model_put_accent: no model at {}", dir.display());
            return;
        }
        let mut m = super::models::AccentModel::load(&dir).expect("load accent model");
        let cases = [
            ("корова", "коров+а"),
            ("телефон", "телеф+он"),
            ("бегемот", "бегем+от"),
            ("карандаш", "каранд+аш"),
            ("программирование", "программ+ирование"),
            ("Москва", "Москв+а"),
            ("стремление", "стремл+ение"),
        ];
        for (w, want) in cases {
            assert_eq!(m.put_accent(w).unwrap(), want, "put_accent({w})");
        }
    }

    // Bit-exact vs Python stress_usage / yo_homograph NER (per-word labels aligned with split_by_words).
    #[test]
    fn bert_ner_predict() {
        let home = match std::env::var_os("RUACCENT_HOME") {
            Some(h) => std::path::PathBuf::from(h),
            None => dirs_cache(),
        };
        let sdir = home.join("nn/nn_stress_usage_predictor");
        let ydir = home.join("nn/nn_yo_homograph_resolver");
        if !sdir.join("model.onnx").exists() || !ydir.join("model.onnx").exists() {
            eprintln!("skip bert_ner_predict: models absent");
            return;
        }
        let mut su = super::models::BertNerModel::load(&sdir).expect("load stress model");
        let mut yo = super::models::BertNerModel::load(&ydir).expect("load yo model");

        let s1 = "Старый замок стоял на горе.";
        assert_eq!(su.predict(s1).unwrap(), ["STRESS", "STRESS", "STRESS", "NO_STRESS", "STRESS", "PUNCT"]);
        assert_eq!(yo.predict(&s1.to_lowercase()).unwrap(), ["NO_YO", "NO_YO", "NO_YO", "NO_YO", "NO_YO", "PUNCT"]);

        let s2 = "Мама мыла раму, а папа читал газету.";
        assert_eq!(
            su.predict(s2).unwrap(),
            ["STRESS", "STRESS", "STRESS", "PUNCT", "NO_STRESS", "STRESS", "STRESS", "STRESS", "PUNCT"]
        );
    }
}
