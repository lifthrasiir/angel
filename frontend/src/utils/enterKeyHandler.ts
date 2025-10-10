/**
 * Common Enter key handler utility
 *
 * Rules:
 * - Ctrl/Meta-Enter: Always send/confirm
 * - Alt/Shift-Enter: Always add newline character
 * - 2+ modifiers: Do nothing
 * - Plain Enter: Add newline if textarea has newlines, otherwise send/confirm
 */

export interface EnterKeyHandlerOptions {
  /** Send/confirm action callback */
  onSendOrConfirm: () => void;
  /** Add newline character callback (optional - allows default behavior by default) */
  onAddNewline?: () => void;
  /** Current text value */
  value: string;
}

/**
 * Enter key event handler function
 *
 * @param e - Keyboard event
 * @param options - Handler options
 * @returns true - Event was handled, false - Allow default behavior
 */
export function handleEnterKey(e: React.KeyboardEvent<HTMLTextAreaElement>, options: EnterKeyHandlerOptions): boolean {
  const { onSendOrConfirm, onAddNewline, value } = options;

  // Do nothing if not Enter key
  if (e.key !== 'Enter') {
    return false;
  }

  // Do nothing if 2+ modifiers
  const numModifiers = (e.ctrlKey ? 1 : 0) + (e.metaKey ? 1 : 0) + (e.altKey ? 1 : 0) + (e.shiftKey ? 1 : 0);
  if (numModifiers >= 2) {
    return false;
  }

  // Ctrl/Meta-Enter: Always send/confirm
  if (e.ctrlKey || e.metaKey) {
    e.preventDefault();
    onSendOrConfirm();
    return true;
  }

  // Alt/Shift-Enter: Always add newline character
  if (e.altKey || e.shiftKey) {
    if (onAddNewline) {
      e.preventDefault();
      onAddNewline();
    }
    // If onAddNewline is not provided, allow default behavior (newline)
    return onAddNewline ? true : false;
  }

  // Plain Enter (no modifiers)
  // Add newline if textarea has newlines, otherwise send/confirm
  if (value.includes('\n')) {
    // If text contains newlines, allow default behavior (add newline)
    return false;
  } else {
    // If no newlines, send/confirm
    e.preventDefault();
    onSendOrConfirm();
    return true;
  }
}
