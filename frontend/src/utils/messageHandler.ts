import { FileAttachment } from '../types/chat';
import { splitOnceByNewline } from './stringUtils';

// SSE Event Types
export const EventSessionID = "S";
export const EventInitialState = "0";
export const EventInitialStateNoCall = "1";
export const EventFunctionCall = "F";
export const EventThought = "T";
export const EventModelMessage = "M";
export const EventFunctionReply = "R";
export const EventComplete = "Q";
export const EventSessionName = "N";
export const EventError = "E";

// EventSource cannot be used directly here because it only supports GET requests,
// and we need to send POST parameters (e.g., systemPrompt which can be very long).
export const sendMessage = async (
  inputMessage: string,
  attachments: FileAttachment[],
  chatSessionId: string | null,
  systemPrompt: string
) => {
  let apiUrl = '';
  let requestBody: any = {};

  if (chatSessionId) {
    apiUrl = '/api/chat/message';
    requestBody = { sessionId: chatSessionId, message: inputMessage, attachments };
  } else {
    apiUrl = '/api/chat/newSessionAndMessage';
    requestBody = { message: inputMessage, systemPrompt: systemPrompt, name: '', attachments };
  }

  const response = await fetch(apiUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(requestBody),
  });

  return response;
};

export interface StreamEventHandlers {
  onMessage: (text: string) => void;
  onThought: (thoughtText: string) => void;
  onFunctionCall: (functionName: string, functionArgs: any) => void;
  onFunctionResponse: (functionResponse: any) => void;
  onSessionUpdate: (sessionId: string) => void;
  onSessionNameUpdate: (sessionId: string, newName: string) => void;
  onEnd: () => void;
  onError: (errorData: string) => void; // Add this line
}

export const processStreamResponse = async (
  response: Response,
  handlers: StreamEventHandlers
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
      let eventString = buffer.substring(0, newlineIndex).slice(6).replace(/\ndata: /g, '\n');
      buffer = buffer.substring(newlineIndex + 2);

      const [type, data] = splitOnceByNewline(eventString);

      if (type === EventModelMessage) {
        handlers.onMessage(data);
      } else if (type === EventThought) {
        const thoughtText = data;
        handlers.onThought(thoughtText);
      } else if (type === EventFunctionCall) {
        const [functionName, functionArgsJson] = splitOnceByNewline(data);
        const functionArgs = JSON.parse(functionArgsJson);
        handlers.onFunctionCall(functionName, functionArgs);
      } else if (type === EventFunctionReply) {
        const functionResponseRaw = JSON.parse(data);
        handlers.onFunctionResponse(functionResponseRaw);
      } else if (type === EventSessionID) {
        const newSessionId = data;
        handlers.onSessionUpdate(newSessionId);
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
      } else {
        console.warn('Unknown protocol:', data);
      }
    }
  }
  return { qReceived, nReceived };
};
