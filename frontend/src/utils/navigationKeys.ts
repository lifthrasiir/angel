/**
 * Navigation keys utility for seamless chat area navigation from chat input
 *
 * This module provides functionality to handle Home/End/PgUp/PgDown keys
 * in the chat input to navigate the chat area without losing focus.
 */

/**
 * Check if a key is a navigation key that should be handled
 */
export function isNavigationKey(e: KeyboardEvent): boolean {
  return ['Home', 'End', 'PageUp', 'PageDown'].includes(e.key);
}

/**
 * Check if a key should trigger textarea focus when not in textarea/input
 */
export function isTextInputKey(e: KeyboardEvent): boolean {
  // Text input keys that should trigger focus to textarea
  // Excludes modifier keys, navigation keys, special keys, and space
  const textInputKeys = /^[a-zA-Z0-9`~!@#$%^&*()\-_=+\[\]{};:'",.<>/?\\|]$/;
  return textInputKeys.test(e.key) && !e.ctrlKey && !e.metaKey && !e.altKey;
}

/**
 * Handle navigation key events in chat input
 * Returns true if the event was handled, false otherwise
 */
export function handleNavigationKeys(
  e: React.KeyboardEvent<HTMLTextAreaElement>,
  textareaRef?: React.RefObject<HTMLTextAreaElement>,
  chatAreaRef?: React.RefObject<HTMLDivElement>,
): boolean {
  // Only handle navigation keys without modifiers
  if (!isNavigationKey(e.nativeEvent) || e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) {
    return false;
  }

  const textarea = textareaRef?.current;
  if (!textarea) {
    return false;
  }

  // Handle Home/End keys with caret movement detection
  if (e.key === 'Home' || e.key === 'End') {
    if (!chatAreaRef?.current) return false;

    const originalSelectionStart = textarea.selectionStart;
    const originalSelectionEnd = textarea.selectionEnd;

    // Let default behavior happen first, then check if caret moved
    requestAnimationFrame(() => {
      const chatArea = chatAreaRef.current;
      if (!chatArea) return;

      // If caret didn't move, perform navigation
      if (textarea.selectionStart === originalSelectionStart && textarea.selectionEnd === originalSelectionEnd) {
        if (e.key === 'Home') {
          chatArea.scrollTo({
            top: 0,
            behavior: 'smooth',
          });
        } else {
          chatArea.scrollTo({
            top: chatArea.scrollHeight,
            behavior: 'smooth',
          });
        }
      }
    });

    // Return false to let default behavior proceed
    return false;
  }

  // Check if textarea can scroll first (only for PgUp/PgDown)
  if (e.key === 'PageUp' || e.key === 'PageDown') {
    const canScrollUp = textarea.scrollTop > 0;
    const canScrollDown = textarea.scrollTop < textarea.scrollHeight - textarea.clientHeight;

    // If textarea can scroll in the direction, allow default behavior
    if ((e.key === 'PageUp' && canScrollUp) || (e.key === 'PageDown' && canScrollDown)) {
      return false;
    }

    // Otherwise, manually scroll the chat area without losing focus
    const chatArea = chatAreaRef?.current;
    if (chatArea) {
      e.preventDefault();

      if (e.key === 'PageUp') {
        chatArea.scrollBy({
          top: -chatArea.clientHeight * 0.8,
          behavior: 'smooth',
        });
      } else if (e.key === 'PageDown') {
        chatArea.scrollBy({
          top: chatArea.clientHeight * 0.8,
          behavior: 'smooth',
        });
      }

      return true;
    }
  }

  return false;
}
