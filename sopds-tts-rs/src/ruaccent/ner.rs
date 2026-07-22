//! Shared token-classification (NER) decode for the stress_usage and yo_homograph models — a faithful
//! port of their identical `collect_pre_entities` + `aggregate_words("AVERAGE")` (ruaccent/
//! stress_usage_model.py, yo_homograph_model.py). Returns one label per word group; the grouping must
//! line up with `split_by_words` (RUAccent indexes the two by the same word position).

use super::tok_bert::Encoding;

/// `scores[i]` = softmax over the classes for token i (special tokens included; skipped here).
pub fn decode(
    sentence: &str,
    enc: &Encoding,
    scores: &[Vec<f32>],
    unk_id: i64,
    id2label: &[String],
) -> Vec<String> {
    // collect_pre_entities: skip special tokens; mark subword pieces. These tokenizers have a
    // continuing_subword_prefix ("##"), so Python's branch is `is_subword = len(word) != len(word_ref)`
    // (char lengths). We slice `word_ref` from the ORIGINAL string by the token's byte offsets.
    let bytes = sentence.as_bytes();
    let mut pre_scores: Vec<&Vec<f32>> = Vec::new();
    let mut pre_subword: Vec<bool> = Vec::new();
    for idx in 0..enc.input_ids.len() {
        if enc.special_tokens_mask[idx] != 0 {
            continue;
        }
        let (s, e) = enc.offsets[idx];
        let word_ref = bytes.get(s..e).and_then(|b| std::str::from_utf8(b).ok()).unwrap_or("");
        let mut is_subword = enc.tokens[idx].chars().count() != word_ref.chars().count();
        if enc.input_ids[idx] == unk_id {
            is_subword = false;
        }
        pre_scores.push(&scores[idx]);
        pre_subword.push(is_subword);
    }
    if pre_scores.is_empty() {
        return Vec::new();
    }

    // aggregate_words: consecutive tokens where the non-first ones are subwords form one word.
    let mut groups: Vec<Vec<usize>> = Vec::new();
    let mut cur: Vec<usize> = Vec::new();
    for i in 0..pre_scores.len() {
        if cur.is_empty() || pre_subword[i] {
            cur.push(i);
        } else {
            groups.push(std::mem::take(&mut cur));
            cur.push(i);
        }
    }
    groups.push(cur); // Python appends the final group unconditionally (pre is non-empty here)

    // aggregate_word("AVERAGE"): mean of the group's score vectors, argmax → id2label.
    groups
        .iter()
        .map(|g| {
            let classes = pre_scores[g[0]].len();
            let mut avg = vec![0f32; classes];
            for &ti in g {
                for (c, a) in avg.iter_mut().enumerate() {
                    *a += pre_scores[ti][c];
                }
            }
            let n = g.len() as f32;
            let mut best = 0usize;
            for c in 0..classes {
                avg[c] /= n;
                if avg[c] > avg[best] {
                    best = c;
                }
            }
            id2label.get(best).cloned().unwrap_or_default()
        })
        .collect()
}
