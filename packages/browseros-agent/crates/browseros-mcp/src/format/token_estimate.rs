const APPROX_CHARS_PER_TOKEN: usize = 3;

#[must_use]
pub fn estimate_text_tokens(text: &str) -> usize {
    text.len().div_ceil(APPROX_CHARS_PER_TOKEN)
}

#[must_use]
pub fn slice_text_by_estimated_tokens(text: &str, max_tokens: usize) -> String {
    if estimate_text_tokens(text) <= max_tokens {
        return text.to_string();
    }

    let mut low = 0;
    let mut high = text.len();
    while low < high {
        let mid = (low + high).div_ceil(2);
        let candidate = floor_char_boundary(text, mid);
        if estimate_text_tokens(&text[..candidate]) <= max_tokens {
            low = candidate;
        } else {
            high = candidate.saturating_sub(1);
        }
    }
    let end = floor_char_boundary(text, low);
    text[..end].to_string()
}

fn floor_char_boundary(text: &str, index: usize) -> usize {
    let mut index = index.min(text.len());
    while !text.is_char_boundary(index) {
        index = index.saturating_sub(1);
    }
    index
}
