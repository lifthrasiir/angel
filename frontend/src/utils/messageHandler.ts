import type { FileAttachment } from '../types/chat';
import { apiFetch } from '../api/apiClient';
import { splitOnceByNewline } from './stringUtils';

// SSE Event Types
//
// Sending initial messages: A -> 0 -> any number of T/M/F/R -> (Q -> N) or E
// Sending subsequent messages: A -> any number of T/M/F/R -> Q or E
// Loading messages and streaming current call: 1 or (0 -> any number of T/M/F/R -> Q/E)

export const EventInitialState = '0';
export const EventInitialStateNoCall = '1';
export const EventAcknowledge = 'A';
export const EventThought = 'T';
export const EventModelMessage = 'M';
export const EventFunctionCall = 'F';
export const EventFunctionReply = 'R';
export const EventComplete = 'Q';
export const EventSessionName = 'N';
export const EventCumulTokenCount = 'C';
export const EventPendingConfirmation = 'P';
export const EventGenerationChanged = 'G';
export const EventError = 'E';

export const sendMessage = async (
  inputMessage: string,
  attachments: FileAttachment[],
  chatSessionId: string | null,
  systemPrompt: string,
  workspaceId?: string,
  primaryBranchId?: string,
  model?: string,
  initialRoots?: string[],
) => {
  let apiUrl = '';
  let requestBody: any = {};

  if (chatSessionId) {
    apiUrl = `/api/chat/${chatSessionId}`;
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

export interface StreamEventHandlers {
  onMessage: (messageId: string, text: string) => void;
  onThought: (messageId: string, thoughtText: string) => void;
  onFunctionCall: (messageId: string, functionName: string, functionArgs: any) => void;
  onFunctionResponse: (
    messageId: string,
    functionName: string,
    functionResponse: any,
    attachments: FileAttachment[],
  ) => void;
  onSessionStart: (sessionId: string, systemPrompt: string, primaryBranchId: string) => void;
  onSessionNameUpdate: (sessionId: string, newName: string) => void;
  onEnd: () => void;
  onError: (errorData: string) => void;
  onAcknowledge: (messageId: string) => void;
  onTokenCount: (messageId: string, cumulTokenCount: number) => void;
  onPendingConfirmation: (data: string) => void;
  onEnvChanged: (messageId: string, envChangedJson: string) => void;
}

export const processStreamResponse = async (
  response: Response,
  handlers: StreamEventHandlers,
): Promise<{ qReceived: boolean; nReceived: boolean }> => {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error('Failed to get readable stream reader.');
  }

  const decoder = new TextDecoder('utf-8');
  let buffer = '';

  let qReceived = false;
  let nReceived = false;

  while (true) {
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

      const [type, data] = splitOnceByNewline(eventString);

      if (type === EventModelMessage) {
        const [messageId, text] = splitOnceByNewline(data);
        handlers.onMessage(messageId, text);
      } else if (type === EventThought) {
        const [messageId, thoughtText] = splitOnceByNewline(data);
        handlers.onThought(messageId, thoughtText);
      } else if (type === EventFunctionCall) {
        const [messageId, rest] = splitOnceByNewline(data);
        const [functionName, functionArgsJson] = splitOnceByNewline(rest);
        const functionArgs = JSON.parse(functionArgsJson);
        handlers.onFunctionCall(messageId, functionName, functionArgs);
      } else if (type === EventFunctionReply) {
        const [messageId, rest] = splitOnceByNewline(data);
        const [functionName, payloadJsonString] = splitOnceByNewline(rest);
        const { response, attachments } = JSON.parse(payloadJsonString);
        handlers.onFunctionResponse(messageId, functionName, response, attachments);
      } else if (type === EventInitialState) {
        // New: Handle EventInitialState
        const { sessionId, systemPrompt, primaryBranchId } = JSON.parse(data);
        handlers.onSessionStart(sessionId, systemPrompt, primaryBranchId);
      } else if (type === EventSessionName) {
        const [sessionIdToUpdate, newName] = splitOnceByNewline(data);
        handlers.onSessionNameUpdate(sessionIdToUpdate, newName);
        nReceived = true;
      } else if (type === EventError) {
        console.error('Stream Error:', data); // Log the error data
        handlers.onError(data); // Call onError handler
        break; // Break the loop on error
      } else if (type === EventComplete) {
        handlers.onEnd();
        qReceived = true;
      } else if (type === EventAcknowledge) {
        handlers.onAcknowledge(data);
      } else if (type === EventCumulTokenCount) {
        const [messageId, cumulTokenCountStr] = splitOnceByNewline(data);
        handlers.onTokenCount(messageId, parseInt(cumulTokenCountStr, 10));
      } else if (type === EventPendingConfirmation) {
        // New event type
        handlers.onPendingConfirmation(data);
        // When a pending confirmation event is received, the stream is paused on the backend.
        // We should not expect EventComplete or other events until confirmation is sent.
        // So, we can break the loop here.
        break;
      } else if (type === EventGenerationChanged) {
        const [messageId, envChangedJson] = splitOnceByNewline(data);
        handlers.onEnvChanged(messageId, envChangedJson);
      } else {
        console.warn('Unknown protocol:', data);
      }
    }
  }
  return { qReceived, nReceived };
};
