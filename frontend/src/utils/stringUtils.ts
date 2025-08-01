/**
 * Splits a string into two parts at the first occurrence of a newline character.
 * If no newline is found, the second part will be an empty string.
 * @param str The string to split.
 * @returns A tuple [beforeFirstNewline, afterFirstNewline].
 */
export function splitOnceByNewline(str: string): [string, string] {
  const firstNewlineIndex = str.indexOf('\n');
  if (firstNewlineIndex !== -1) {
    return [str.substring(0, firstNewlineIndex), str.substring(firstNewlineIndex + 1)];
  }
  return [str, ''];
}

