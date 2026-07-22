//! Text preprocessing ported 1:1 from RUAccent (`TextPreprocessor` + `ruaccent.py` helpers +
//! `text_postprocessor.fix_capital`). Bit-exact parity is the goal — see
//! docs/decisions/004-ruaccent-rust-port.md. Sentence splitting (razdel) lands in Phase 3.

use regex::Regex;
use std::sync::OnceLock;

const VOWELS: &str = "аеёиоуыэюяАЕЁИОУЫЭЮЯ";
const PUNCT: &str = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~";

/// ruaccent.py:24 `self.normalize` — drop any char outside the allowed set. Implemented as a char
/// filter equivalent to the negated char-class (avoids escaping the typographic quotes in a regex).
pub fn normalize(text: &str) -> String {
    text.chars().filter(|&c| is_allowed(c)).collect()
}

fn is_allowed(c: char) -> bool {
    c.is_ascii_alphanumeric()
        || c.is_whitespace()
        || ('а'..='я').contains(&c)
        || ('А'..='Я').contains(&c)
        || c == 'ё'
        || c == 'Ё'
        || matches!(
            c,
            '—' | '.' | ',' | '!' | '?' | ':' | ';'
                | '"' | '\'' | '(' | ')' | '{' | '}' | '[' | ']'
                | '«' | '»' | '„' | '“' | '”' | '‘' | '’' | '-'
        )
}

pub fn count_vowels(text: &str) -> usize {
    text.chars().filter(|c| VOWELS.contains(*c)).count()
}

pub fn has_punctuation(text: &str) -> bool {
    text.chars().any(|c| PUNCT.contains(c))
}

/// text_postprocessor.fix_capital — apply `source`'s upper/lower-case mask onto `target`
/// (used to keep a dict/model replacement in the original word's case). If lengths differ,
/// return `target` unchanged.
pub fn fix_capital(source: &str, target: &str) -> String {
    let s: Vec<char> = source.chars().collect();
    let t: Vec<char> = target.chars().collect();
    if s.len() != t.len() {
        return target.to_string();
    }
    s.iter()
        .zip(t.iter())
        .map(|(sc, tc)| {
            if sc.is_uppercase() {
                tc.to_uppercase().to_string()
            } else {
                tc.to_lowercase().to_string()
            }
        })
        .collect()
}

/// ruaccent.py delete_spaces_before_punc — collapse a space before punctuation, and restore the
/// `~` placeholder (see split_by_words) back to `-`.
pub fn delete_spaces_before_punc(text: &str) -> String {
    const P: &str = "!\"#$%&'()*,./:;<=>?@[\\]^_`{|}-";
    let mut t = text.to_string();
    for ch in P.chars() {
        if ch == '-' {
            t = t.replace(&format!(" {ch}"), &ch.to_string());
            t = t.replace(&format!("{ch} "), &ch.to_string());
        }
        t = t.replace(&format!(" {ch}"), &ch.to_string());
    }
    t.replace('~', "-")
}

// RUAccent's tokenizer regex is `\w*(?:\+\w+)*|[^\w\s]+`, whose `\w*` matches empty everywhere; the
// empty hits are filtered out by `words_mask` in Python. We can't reproduce that filtered result with
// Rust's `find_iter`: unlike CPython 3.7+ `finditer` (which sets `must_advance` after an empty match and
// so reaches `[^\w\s]+` for a punctuation run), Rust's engine reports the empty `\w*` and never tries the
// second alternative — so bare punctuation like "!!!" or a trailing "." would be dropped. This rewrite is
// the NON-empty subset of the original (verified token-for-token against Python finditer): a word is
// `\w+` with optional `+\w+` groups, OR a `+`-leading stressed word, OR a punctuation run. No empty
// matches, so `find_iter` yields exactly Python's `words_mask` tokens.
fn word_re() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(r"\w+(?:\+\w+)*|(?:\+\w+)+|[^\w\s]+").unwrap())
}

/// TextPreprocessor.split_by_words — tokenize into `words` (kept in original case) plus the
/// interleaved `remaining_text` separators, such that
/// `zip(remaining_text, words).flat() + [remaining_text.last()]` losslessly rejoins the string
/// (after the `" - " -> " ~ "` substitution). The regex runs on the lowercased text; slices are
/// taken from the original. Empty regex matches are dropped (they only affect which separator span
/// a gap lands in; the concatenation is invariant to them).
pub fn split_by_words(input: &str) -> (Vec<String>, Vec<String>) {
    let string = input.replace(" - ", " ~ ");
    let lower = string.to_lowercase();
    // NB: match on `lower`, slice from `string` at the same byte range — the lowercasing here is
    // length-preserving (Cyrillic/Latin), same assumption as the Python. `word_re()` yields only
    // non-empty tokens (see its comment), matching Python's `words_mask`. Separators are rebuilt
    // from the gaps between consecutive spans (equivalent to Python's `remaining_text` logic).
    let spans: Vec<(usize, usize)> = word_re()
        .find_iter(&lower)
        .map(|m| (m.start(), m.end()))
        .collect();
    let words: Vec<String> = spans.iter().map(|&(s, e)| slice(&string, s, e)).collect();

    if spans.is_empty() {
        return (words, vec![String::new(), String::new()]);
    }
    let mut rem: Vec<String> = Vec::with_capacity(spans.len() + 1);
    rem.push(slice(&string, 0, spans[0].0)); // before the first word
    for w in spans.windows(2) {
        rem.push(slice(&string, w[0].1, w[1].0)); // between consecutive words
    }
    rem.push(slice(&string, spans.last().unwrap().1, string.len())); // after the last word
    (words, rem)
}

/// Insert `+` into `word` at the char positions where `stressed` has a `+`, preserving `word`'s
/// case. Faithful to ruaccent.py `_process_accent`'s finditer loop (including its accumulation).
pub fn transfer_plus(word: &str, stressed: &str) -> String {
    let word_chars: Vec<char> = word.chars().collect();
    let plus: Vec<(usize, usize)> = stressed
        .chars()
        .enumerate()
        .filter(|(_, c)| *c == '+')
        .map(|(i, _)| (i, i + 1))
        .collect();
    let mut fixed: Vec<char> = word_chars.clone();
    for (j, &(start, end)) in plus.iter().enumerate() {
        let left_end = (start + j).min(fixed.len());
        let right_start = (end - 1).min(word_chars.len());
        let mut nw: Vec<char> = fixed[..left_end].to_vec();
        nw.push('+');
        nw.extend_from_slice(&word_chars[right_start..]);
        fixed = nw;
    }
    fixed.into_iter().collect()
}

// byte-range slice that clamps to char boundaries (defensive; ranges come from the regex on `lower`).
fn slice(s: &str, start: usize, end: usize) -> String {
    let (a, b) = (start.min(s.len()), end.min(s.len()));
    if a >= b || !s.is_char_boundary(a) || !s.is_char_boundary(b) {
        return String::new();
    }
    s[a..b].to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rejoin(words: &[String], rem: &[String]) -> String {
        let mut out = String::new();
        for (l, r) in rem.iter().zip(words.iter()) {
            out.push_str(l);
            out.push_str(r);
        }
        out.push_str(rem.last().unwrap());
        out
    }

    #[test]
    fn split_roundtrip() {
        for s in [
            "Старинный замок стоял на горе.",
            "Он ещё запер замок, испытывая муки.",
            "Текст с - тире и «кавычками».",
            "",
            "!!!",
        ] {
            let expect = s.replace(" - ", " ~ ");
            let (w, r) = split_by_words(s);
            assert_eq!(rejoin(&w, &r), expect, "roundtrip failed for {s:?}");
        }
    }

    // Exact words/rem pinned against Python RUAccent `TextPreprocessor.split_by_words` (ground truth).
    #[test]
    fn split_matches_python() {
        let cases: &[(&str, &[&str], &[&str])] = &[
            ("замок.", &["замок", "."], &["", "", ""]),
            (
                "Старинный замок стоял на горе.",
                &["Старинный", "замок", "стоял", "на", "горе", "."],
                &["", " ", " ", " ", " ", "", ""],
            ),
            ("!!!", &["!!!"], &["", ""]),
            ("а, б!", &["а", ",", "б", "!"], &["", "", " ", "", ""]),
        ];
        for (s, words, rem) in cases {
            let (w, r) = split_by_words(s);
            assert_eq!(w, *words, "words for {s:?}");
            assert_eq!(r, *rem, "rem for {s:?}");
        }
    }

    #[test]
    fn normalize_strips() {
        assert_eq!(normalize("абв🎧xyz—«»"), "абвxyz—«»");
        assert_eq!(normalize("ёЁ"), "ёЁ");
    }

    #[test]
    fn transfer_plus_case() {
        assert_eq!(transfer_plus("Коса", "к+оса"), "К+оса");
        assert_eq!(transfer_plus("замок", "зам+ок"), "зам+ок");
    }

    #[test]
    fn vowels_and_punct() {
        assert_eq!(count_vowels("замок"), 2);
        assert!(has_punctuation("а,б"));
        assert!(!has_punctuation("абв"));
    }
}
