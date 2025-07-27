export function smartPreprocessMarkdown(markdownText: string): string {
  // Regular expression to identify fenced code blocks (multiline, non-greedy)
  // The `g` flag finds all matches, and the `s` flag allows `.` to include newline characters.
  const fencedBlockRegex = /(```[\s\S]*?```)/g;

  let parts: string[] = [];
  let lastIndex = 0;

  // Split Markdown text into fenced blocks and other parts
  markdownText.replace(fencedBlockRegex, (match, p1, offset) => {
    // Add any unprocessed text before the current fenced block
    if (offset > lastIndex) {
      parts.push(markdownText.substring(lastIndex, offset));
    }
    // Add the fenced block itself as is
    parts.push(p1);
    lastIndex = offset + match.length;
    return match;
  });

  // Add any remaining text after the last fenced block
  if (lastIndex < markdownText.length) {
    parts.push(markdownText.substring(lastIndex));
  }

  // Process each separated part
  const processedParts = parts.map(part => {
    if (part.startsWith('```') && part.endsWith('```')) {
      // Return fenced block as is
      return part;
    } else {
      // `)**xxx` is not a valid right-flanking delimiter run in CommonMark which can't be preceded by punctuation,
      // but commonly occurs in the LLM-generated CJK text (cf. https://talk.commonmark.org/t/2528).
      // We relax the specification by allowing a certain closing punctuation before the delimiter run.
      return part.replace(/([)\]])(\*\*|\*)(\p{L})/ug, '$1$2<!-- -->$3');
    }
  });

  // Recombine the processed parts
  return processedParts.join('');
}