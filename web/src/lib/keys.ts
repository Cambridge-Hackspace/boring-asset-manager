// Keys made from what people read (#171): a question's, a hook's, a choice's
// value. They are made as field keys are, from the label as it is typed,
// until someone changes the key by hand.

/** Text as a key's letters: lowercase, accents dropped, anything else one _, none at either end. */
function letters(text: string): string {
  return text
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

/**
 * A key made from a label, as questions and hooks take them: lowercase
 * letters, digits and _, starting with a letter, at most max long. Empty
 * when the label has no letter to start with.
 */
export function keyFrom(label: string, max = 64): string {
  return letters(label)
    .replace(/^[^a-z]+/, '')
    .slice(0, max)
    .replace(/_+$/, '');
}

/**
 * A choice's value made from its label: as a key, though it may start with
 * a digit, and the label itself, trimmed, when it has no letters or digits
 * at all, since a choice's value may be any text.
 */
export function valueFrom(label: string): string {
  return letters(label).slice(0, 255).replace(/_+$/, '') || label.trim();
}
