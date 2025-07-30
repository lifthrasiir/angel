import { ChatMessage, FileAttachment } from '../types/chat';

export const updateMessagesState = (
  messages: ChatMessage[],
  newMessage: ChatMessage,
  options?: { replaceId?: string; insertBeforeAgentId?: string }
): ChatMessage[] => {
  const newMessages = [...messages];
  let insertIndex = newMessages.length;

  if (options?.replaceId) {
    const indexToReplace = newMessages.findIndex(msg => msg.id === options.replaceId);
    if (indexToReplace !== -1) {
      newMessages[indexToReplace] = newMessage;
      return newMessages;
    }
  }

  if (options?.insertBeforeAgentId) {
    const agentMessageIndex = newMessages.findIndex(msg => msg.id === options.insertBeforeAgentId);
    if (agentMessageIndex !== -1) {
      insertIndex = agentMessageIndex;
    }
  }

  newMessages.splice(insertIndex, 0, newMessage);
  return newMessages;
};

export const processStreamingMessage = (
  messages: ChatMessage[],
  agentMessageId: string,
  currentAgentText: string
): ChatMessage[] => {
  return messages.map(msg =>
    msg.id === agentMessageId
      ? { ...msg, parts: [{ text: currentAgentText }] } // Create a new object for the agent message
      : msg
  );
};

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

      const data = eventString.slice(6).replace(/\ndata: /g, '\n');

      if (data.startsWith('M\n')) {
        handlers.onMessage(data.substring(2));
      } else if (data.startsWith('T\n')) {
        const thoughtText = data.substring(2);
        const thoughtId = crypto.randomUUID();
        handlers.onThought(thoughtText, thoughtId);
      } else if (data.startsWith('F\n')) {
        const [functionName, functionArgsJson] = data.substring(2).split('\n', 2);
        const functionArgs = JSON.parse(functionArgsJson);
        handlers.onFunctionCall(functionName, functionArgs);
      } else if (data.startsWith('R')) {
        const functionResponseRaw = JSON.parse(data.substring(2));
        handlers.onFunctionResponse(functionResponseRaw);
      } else if (data.startsWith('S\n')) {
        const newSessionId = data.substring(2);
        handlers.onSessionUpdate(newSessionId);
      } else if (data.startsWith('N\n')) {
        const [sessionIdToUpdate, newName] = data.substring(2).split('\n', 2);
        handlers.onSessionNameUpdate(sessionIdToUpdate, newName);
      } else if (data === 'Q') {
        handlers.onEnd();
        break;
      } else {
        console.warn('Unknown protocol:', data);
      }
    }
  }
};