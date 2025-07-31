import { FileAttachment } from '../types/chat';

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
  onThought: (thoughtText: string, thoughtId: string) => void;
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
) => {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error('Failed to get readable stream reader.');
  }

  const decoder = new TextDecoder('utf-8');
  let buffer = '';

  while (true) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });

    let newlineIndex;
    while ((newlineIndex = buffer.indexOf('\n\n')) !== -1) {
      const eventString = buffer.substring(0, newlineIndex);
      buffer = buffer.substring(newlineIndex + 2);

      const [type, data] = eventString.slice(6).replace(/\ndata: /g, '\n').split('\n', 2);

      if (type === 'M') {
        handlers.onMessage(data);
      } else if (type === 'T') {
        const thoughtText = data;
        const thoughtId = crypto.randomUUID();
        handlers.onThought(thoughtText, thoughtId);
      } else if (type === 'F') {
        const [functionName, functionArgsJson] = data.split('\n', 2);
        const functionArgs = JSON.parse(functionArgsJson);
        handlers.onFunctionCall(functionName, functionArgs);
      } else if (type === 'R') {
        const functionResponseRaw = JSON.parse(data);
        handlers.onFunctionResponse(functionResponseRaw);
      } else if (type === 'S') {
        const newSessionId = data;
        handlers.onSessionUpdate(newSessionId);
      } else if (type === 'N') {
        const [sessionIdToUpdate, newName] = data.split('\n', 2);
        handlers.onSessionNameUpdate(sessionIdToUpdate, newName);
      } else if (type === 'E') {
        // Error
        console.error('Stream Error:', data); // Log the error data
        handlers.onError(data); // Call onError handler
        handlers.onEnd(); // Call onEnd handler for cleanup
        break; // Break the loop on error
      } else if (type === 'Q') {
        handlers.onEnd();
        break;
      } else {
        console.warn('Unknown protocol:', data);
      }
    }
  }
};
