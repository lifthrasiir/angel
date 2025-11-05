import type { FileAttachment } from '../types/chat';
import { apiFetch } from '../api/apiClient';
import {
  EventComplete,
  EventSessionName,
  EventPendingConfirmation,
  EventError,
  parseSseEvent,
  type SseEvent,
} from '../types/events';

export const sendMessage = async (
  inputMessage: string,
  attachments: FileAttachment[],
  chatSessionId: string | null,
  systemPrompt: string,
  workspaceId?: string,
  primaryBranchId?: string,
  model?: string,
  initialRoots?: string[],
  beforeMessageId?: string,
) => {
  let apiUrl = '';
  let requestBody: any = {};

  if (chatSessionId) {
    apiUrl = `/api/chat/${chatSessionId}`;
    if (beforeMessageId) {
      apiUrl += `?beforeMessageID=${beforeMessageId}`;
    }
    requestBody = { message: inputMessage, attachments, model };
    if (primaryBranchId) {
      requestBody.primaryBranchId = primaryBranchId;
    }
  } else {
    apiUrl = '/api/chat';
    requestBody = {
      message: inputMessage,
      systemPrompt: systemPrompt,
      name: '',
      attachments,
      model,
    };
    if (workspaceId) {
      requestBody.workspaceId = workspaceId;
    }
    if (initialRoots) {
      requestBody.initialRoots = initialRoots;
    }
  }

  const response = await apiFetch(apiUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestBody),
  });

  return response;
};

export type SseEventHandler = (event: SseEvent) => void;

export const processStreamResponse = async (
  response: Response,
  handleEvent: SseEventHandler,
  abortSignal?: AbortSignal,
): Promise<{ qReceived: boolean; nReceived: boolean }> => {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error('Failed to get readable stream reader.');
  }

  const decoder = new TextDecoder('utf-8');
  let buffer = '';

  let qReceived = false;
  let nReceived = false;

  streamLoop: while (true) {
    // Check if the operation was aborted
    if (abortSignal?.aborted) {
      console.log('🚫 Stream processing aborted, cancelling reader');
      reader.cancel();
      throw new DOMException('Stream processing was aborted', 'AbortError');
    }

    const { done, value } = await reader.read();
    if (done) {
      break; // Exit loop immediately if stream is done
    }
    buffer += decoder.decode(value, { stream: true });

    let newlineIndex;
    while ((newlineIndex = buffer.indexOf('\n\n')) !== -1) {
      const eventString = buffer
        .substring(0, newlineIndex)
        .slice(6)
        .replace(/\ndata: /g, '\n');
      buffer = buffer.substring(newlineIndex + 2);

      const sseEvent = parseSseEvent(eventString);

      // Call the event handler for all events
      handleEvent(sseEvent);

      // Handle special events that affect the stream state
      switch (sseEvent.type) {
        case EventComplete:
          qReceived = true;
          break;
        case EventSessionName:
          nReceived = true;
          break;
        case EventPendingConfirmation:
          // When a pending confirmation event is received, the stream is paused on the backend.
          // We should not expect EventComplete or other events until confirmation is sent.
          break streamLoop;
        case EventError:
          console.error('Stream Error:', sseEvent.error); // Log the error data
          break streamLoop; // Break the loop on error
      }
    }
  }
  return { qReceived, nReceived };
};
