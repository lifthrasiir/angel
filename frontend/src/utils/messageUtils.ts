import { FileAttachment } from '../types/chat';

/**
 * Check if a message is image-only (no text content but has image attachments)
 */
export function isImageOnlyMessage(text?: string, attachments?: FileAttachment[]): boolean {
  return Boolean(
    (!text || text.trim() === '') &&
      attachments &&
      attachments.length > 0 &&
      attachments.some((att) => att.mimeType.startsWith('image/')),
  );
}
