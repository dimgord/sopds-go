//! Faithful port of razdel's rule-based Russian sentence segmenter (`razdel/segmenters/sentenize.py`
//! + split/base/rule/punct/sokr/substring), plus RUAccent's `TextPreprocessor.split_by_sentences`
//! reconstruction on top. Required for bit-exact multi-sentence parity (RUAccent runs the NER models
//! per sentence, so sentence boundaries must match Python 1:1).
//!
//! Design mirrors razdel exactly: a delimiter regex yields candidate split points; at each one a set
//! of ordered rules votes JOIN (merge) or abstains; the segmenter accumulates a buffer, emitting a
//! sentence whenever no rule joins. Windows around each delimiter are the same 10-CHARACTER context
//! razdel uses (so `LAST_TOKEN`/`FIRST_TOKEN`/etc. see the same text).

use std::collections::HashSet;
use std::sync::OnceLock;

use regex::Regex;

// ── punct.py ─────────────────────────────────────────────────────────────────────────────────────
const ENDINGS: &str = ".?!…";
const DASHES: &str = "‑–—−-";
const CLOSE_QUOTES: &str = "»”’";
const GENERIC_QUOTES: &str = "\"„'";
const QUOTES_OPEN: &str = "«“‘";
const CLOSE_BRACKETS: &str = ")]}";
const SMILES: &str = r"[=:;]-?[)(]{1,3}";

// sentenize.py
const BULLET_BOUNDS: &str = ".)";
const BULLET_SIZE: usize = 20;
fn bullet_chars() -> &'static HashSet<char> {
    static S: OnceLock<HashSet<char>> = OnceLock::new();
    S.get_or_init(|| "§абвгдеabcdef".chars().collect())
}

fn delimiters() -> String {
    // ENDINGS + ';' + GENERIC_QUOTES + CLOSE_QUOTES + CLOSE_BRACKETS
    format!("{ENDINGS};{GENERIC_QUOTES}{CLOSE_QUOTES}{CLOSE_BRACKETS}")
}

fn quotes_all() -> String {
    format!("{QUOTES_OPEN}{CLOSE_QUOTES}{GENERIC_QUOTES}")
}

// ── regexes ──────────────────────────────────────────────────────────────────────────────────────
macro_rules! re {
    ($f:ident, $p:expr) => {
        fn $f() -> &'static Regex {
            static RE: OnceLock<Regex> = OnceLock::new();
            RE.get_or_init(|| Regex::new($p).unwrap())
        }
    };
}
re!(re_first_token, r"^\s*([^\W\d]+|\d+|[^\w\s])");
re!(re_last_token, r"([^\W\d]+|\d+|[^\w\s])\s*$");
re!(re_token, r"([^\W\d]+|\d+|[^\w\s])");
re!(re_word, r"([^\W\d]+|\d+)");
re!(re_pair_sokr, r"(\w)\s*\.\s*(\w)\s*$");
re!(re_roman, r"^[IVXML]+$");

fn re_delimiter() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(&format!("({}|[{}])", SMILES, regex::escape(&delimiters()))).unwrap())
}
fn re_smile_prefix() -> &'static Regex {
    static RE: OnceLock<Regex> = OnceLock::new();
    RE.get_or_init(|| Regex::new(&format!(r"^\s*{SMILES}")).unwrap())
}

// ── sokr.py sets ─────────────────────────────────────────────────────────────────────────────────
fn words(lines: &[&str]) -> HashSet<String> {
    lines.iter().flat_map(|l| l.split_whitespace()).map(str::to_string).collect()
}
fn pairs(lines: &[&str]) -> HashSet<(String, String)> {
    lines
        .iter()
        .filter_map(|l| {
            let p: Vec<&str> = l.split_whitespace().collect();
            if p.len() == 2 {
                Some((p[0].to_string(), p[1].to_string()))
            } else {
                None // razdel has one malformed 3-token entry ("жен рмуж р") — never matches a pair lookup
            }
        })
        .collect()
}

fn head_sokrs() -> &'static HashSet<String> {
    static S: OnceLock<HashSet<String>> = OnceLock::new();
    S.get_or_init(|| {
        words(&[
            "букв", "ст", "трад", "лат венг исп кат укр нем англ фр итал греч",
            "евр араб яп слав кит рус русск латв", "словацк хорв", "mr mrs ms dr vs", "св",
            "арх зав зам проф акад", "кн", "корр", "ред", "гр", "ср", "чл корр", "им", "тов",
            "нач пол", "chap", "п пп ст ч чч гл стр абз пт", "no",
            "просп пр ул ш г гор д стр к корп пер корп обл эт пом ауд оф ком комн каб",
            "домовлад лит", "т", "рп пос с х", "пл", "bd", "о оз", "р", "а", "обр", "ум", "ок",
            "откр", "пс ps", "upd", "см", "напр", "доп", "юр физ", "тел", "сб", "внутр", "дифф",
            "гос", "отм",
        ])
    })
}
fn sokrs() -> &'static HashSet<String> {
    // SOKRS = TAIL_SOKRS | HEAD_SOKRS | OTHER_SOKRS
    static S: OnceLock<HashSet<String>> = OnceLock::new();
    S.get_or_init(|| {
        let mut s = words(&[
            "дес тыс млн млрд", "дол долл", "коп руб р", "проц", "га", "барр", "куб", "кв км", "см",
            "час мин сек", "в вв", "г гг", "с стр", "co corp inc", "изд ed", "др", "al",
        ]);
        s.extend(head_sokrs().iter().cloned());
        s.extend(words(&["сокр рис искл прим", "яз", "устар", "шутл"]));
        s
    })
}
fn pair_sokrs() -> &'static HashSet<(String, String)> {
    // PAIR_SOKRS = TAIL_PAIR_SOKRS | HEAD_PAIR_SOKRS | OTHER_PAIR_SOKRS
    static S: OnceLock<HashSet<(String, String)>> = OnceLock::new();
    S.get_or_init(|| {
        let mut s = pairs(&[
            "т п", "т д", "у е", "н э", "p m", "a m", "с г", "р х", "с г", "с ш", "з д", "л с",
            "ч т", "т д",
        ]);
        s.extend(pairs(&["т е", "т к", "т н", "и о", "к н", "к п", "п н", "к т", "т н", "л д"]));
        s.extend(pairs(&["ед ч", "мн ч", "повел накл"]));
        s
    })
}
fn head_pair_sokrs() -> &'static HashSet<(String, String)> {
    static S: OnceLock<HashSet<(String, String)>> = OnceLock::new();
    S.get_or_init(|| pairs(&["т е", "т к", "т н", "и о", "к н", "к п", "п н", "к т", "т н", "л д"]))
}
fn initials() -> &'static HashSet<String> {
    static S: OnceLock<HashSet<String>> = OnceLock::new();
    S.get_or_init(|| ["дж", "ed", "вс"].iter().map(|s| s.to_string()).collect())
}

// ── string predicates (Python str.isalpha/islower/isupper/isdigit semantics) ────────────────────
fn is_alpha(s: &str) -> bool {
    !s.is_empty() && s.chars().all(|c| c.is_alphabetic())
}
fn is_digit(s: &str) -> bool {
    !s.is_empty() && s.chars().all(|c| c.is_ascii_digit())
}
fn is_lower(s: &str) -> bool {
    let mut cased = false;
    for c in s.chars() {
        if c.is_uppercase() {
            return false;
        }
        if c.is_lowercase() {
            cased = true;
        }
    }
    cased
}
fn is_upper(s: &str) -> bool {
    let mut cased = false;
    for c in s.chars() {
        if c.is_lowercase() {
            return false;
        }
        if c.is_uppercase() {
            cased = true;
        }
    }
    cased
}
fn is_lower_alpha(s: &str) -> bool {
    is_alpha(s) && is_lower(s)
}
fn is_sokr(token: &str) -> bool {
    if is_digit(token) {
        return true;
    }
    if !is_alpha(token) {
        return true; // punct
    }
    is_lower(token)
}
fn is_bullet(token: &str) -> bool {
    if is_digit(token) {
        return true;
    }
    if BULLET_BOUNDS.contains(token) {
        return true; // token in ".)"
    }
    let tl = token.to_lowercase();
    if tl.chars().count() == 1 && bullet_chars().contains(&tl.chars().next().unwrap()) {
        return true; // token.lower() in BULLET_CHARS
    }
    re_roman().is_match(token)
}

/// One candidate split: the delimiter plus its 10-char left/right windows and the accumulated buffer.
struct Split<'a> {
    left: String,
    delimiter: &'a str,
    right: String,
    buffer: String,
}

impl Split<'_> {
    fn first_token(s: &str) -> Option<String> {
        re_first_token().captures(s).map(|c| c[1].to_string())
    }
    fn last_token(&self) -> Option<String> {
        re_last_token().captures(&self.left).map(|c| c[1].to_string())
    }
    fn right_token(&self) -> Option<String> {
        Self::first_token(&self.right)
    }
    fn left_pair_sokr(&self) -> Option<(String, String)> {
        re_pair_sokr().captures(&self.left).map(|c| (c[1].to_string(), c[2].to_string()))
    }
    fn right_word(&self) -> Option<String> {
        re_word().captures(&self.right).map(|c| c[1].to_string())
    }
    fn right_space_prefix(&self) -> bool {
        self.right.chars().next().map(|c| c.is_whitespace()).unwrap_or(false)
    }
    fn left_space_suffix(&self) -> bool {
        self.left.chars().last().map(|c| c.is_whitespace()).unwrap_or(false)
    }
    fn buffer_tokens(&self) -> Vec<String> {
        re_token().captures_iter(&self.buffer).map(|c| c[1].to_string()).collect()
    }
}

// ── rules (each returns true == JOIN; None-equivalent is `false` meaning "abstain") ──────────────
// join() below stops at the first rule that "fires"; a rule fires by returning Some(true)=JOIN.
// (razdel rules only ever return JOIN or None, never SPLIT.)
type Vote = Option<bool>;
const JOIN: Vote = Some(true);

fn close_bound(split: &Split) -> Vote {
    match split.last_token() {
        Some(t) if ENDINGS.contains(t.as_str()) => None,
        _ => JOIN,
    }
}

fn join(split: &Split) -> bool {
    let rules: [fn(&Split) -> Vote; 11] = [
        // empty_side
        |s| if s.last_token().is_none() || s.right_token().is_none() { JOIN } else { None },
        // no_space_prefix
        |s| if !s.right_space_prefix() { JOIN } else { None },
        // lower_right
        |s| match s.right_token() {
            Some(t) if is_lower_alpha(&t) => JOIN,
            _ => None,
        },
        // delimiter_right
        |s| {
            let right = match s.right_token() {
                Some(t) => t,
                None => return None,
            };
            if GENERIC_QUOTES.contains(right.as_str()) {
                return None;
            }
            if delimiters().contains(right.as_str()) {
                return JOIN;
            }
            if re_smile_prefix().is_match(&s.right) {
                return JOIN;
            }
            None
        },
        // sokr_left
        |s| {
            if s.delimiter != "." {
                return None;
            }
            let right = s.right_token();
            if let Some((a, b)) = s.left_pair_sokr() {
                let left = (a.to_lowercase(), b.to_lowercase());
                if head_pair_sokrs().contains(&left) {
                    return JOIN;
                }
                if pair_sokrs().contains(&left) {
                    if right.as_deref().map(is_sokr).unwrap_or(false) {
                        return JOIN;
                    }
                    return None;
                }
            }
            let left = s.last_token().map(|t| t.to_lowercase()).unwrap_or_default();
            if head_sokrs().contains(&left) {
                return JOIN;
            }
            if sokrs().contains(&left) && right.as_deref().map(is_sokr).unwrap_or(false) {
                return JOIN;
            }
            None
        },
        // inside_pair_sokr
        |s| {
            if s.delimiter != "." {
                return None;
            }
            let left = s.last_token().map(|t| t.to_lowercase()).unwrap_or_default();
            let right = s.right_token().map(|t| t.to_lowercase()).unwrap_or_default();
            if pair_sokrs().contains(&(left, right)) {
                return JOIN;
            }
            None
        },
        // initials_left
        |s| {
            if s.delimiter != "." {
                return None;
            }
            let left = match s.last_token() {
                Some(t) => t,
                None => return None,
            };
            if is_upper(&left) && left.chars().count() == 1 {
                return JOIN;
            }
            if initials().contains(&left.to_lowercase()) {
                return JOIN;
            }
            None
        },
        // list_item
        |s| {
            if !(s.delimiter.chars().count() == 1 && BULLET_BOUNDS.contains(s.delimiter)) {
                return None;
            }
            if s.buffer.chars().count() > BULLET_SIZE {
                return None;
            }
            if s.buffer_tokens().iter().all(|t| is_bullet(t)) {
                return JOIN;
            }
            None
        },
        // close_quote
        |s| {
            if !quotes_all().contains(s.delimiter) {
                return None;
            }
            if CLOSE_QUOTES.contains(s.delimiter) {
                return close_bound(s);
            }
            if GENERIC_QUOTES.contains(s.delimiter) {
                if !s.left_space_suffix() {
                    return close_bound(s);
                }
                return JOIN;
            }
            None
        },
        // close_bracket
        |s| {
            if CLOSE_BRACKETS.contains(s.delimiter) {
                return close_bound(s);
            }
            None
        },
        // dash_right
        |s| {
            let rt = match s.right_token() {
                Some(t) => t,
                None => return None,
            };
            if !DASHES.contains(rt.as_str()) {
                return None;
            }
            match s.right_word() {
                Some(w) if is_lower_alpha(&w) => JOIN,
                _ => None,
            }
        },
    ];
    for rule in rules {
        if let Some(action) = rule(split) {
            return action; // action is always JOIN(true); no rule emits SPLIT
        }
    }
    false
}

/// Last ≤10 chars of `s` (razdel's `text[start-10:start]` left window).
fn window_left(s: &str) -> String {
    let n = s.chars().count();
    s.chars().skip(n.saturating_sub(10)).collect()
}
/// First ≤10 chars of `s` (razdel's `text[stop:stop+10]` right window).
fn window_right(s: &str) -> String {
    s.chars().take(10).collect()
}

/// razdel `sentenize` → substrings (byte-offset start/stop into `text`, stripped text). Matches
/// Python's `find_substrings(post(segment(split(text))), text)`.
pub struct Substring {
    pub start: usize,
    pub stop: usize,
    pub text: String,
}

pub fn sentenize(text: &str) -> Vec<Substring> {
    // split(): delimiter matches → interleaved chunks / splits.
    let mut delims: Vec<(usize, usize, &str)> = Vec::new();
    for m in re_delimiter().find_iter(text) {
        delims.push((m.start(), m.end(), m.as_str()));
    }
    let mut chunks: Vec<String> = Vec::new();
    let mut prev = 0usize;
    for &(start, stop, _) in &delims {
        chunks.push(text[prev..start].to_string());
        prev = stop;
    }
    chunks.push(text[prev..].to_string());

    // segment(): accumulate a buffer, emit when no rule joins.
    let mut sentences: Vec<String> = Vec::new();
    let mut buffer = chunks[0].clone();
    for (i, &(start, stop, delim)) in delims.iter().enumerate() {
        let split = Split {
            left: window_left(&text[..start]),
            delimiter: delim,
            right: window_right(&text[stop..]),
            buffer: buffer.clone(),
        };
        let right_chunk = &chunks[i + 1];
        if join(&split) {
            buffer = buffer + delim + right_chunk;
        } else {
            sentences.push(buffer + delim);
            buffer = right_chunk.clone();
        }
    }
    sentences.push(buffer);

    // post(): strip. find_substrings(): map each stripped chunk back to `text`.
    let mut out = Vec::with_capacity(sentences.len());
    let mut offset = 0usize;
    for s in &sentences {
        let chunk = s.trim();
        let start = text[offset..].find(chunk).map(|p| offset + p).unwrap_or(offset);
        let stop = start + chunk.len();
        out.push(Substring { start, stop, text: chunk.to_string() });
        offset = stop;
    }
    out
}

/// RUAccent `TextPreprocessor.split_by_sentences`: sentences with the inter-sentence gap prepended, so
/// the pieces concatenate back to `text` exactly.
pub fn split_by_sentences(text: &str) -> Vec<String> {
    let sents = sentenize(text);
    if sents.is_empty() {
        return Vec::new();
    }
    let mut result: Vec<String> = Vec::with_capacity(sents.len());
    let mut prev_stop = 0usize;
    for s in &sents {
        if prev_stop != s.start {
            result.push(format!("{}{}", &text[prev_stop..s.start], s.text));
        } else {
            result.push(s.text.clone());
        }
        prev_stop = s.stop;
    }
    let last = result.len() - 1;
    result[last].push_str(&text[sents[sents.len() - 1].stop..]);
    result
}
