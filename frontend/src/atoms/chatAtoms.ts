import { atom } from 'jotai';
import type { ChatMessage, Session } from '../types/chat';

// Core chat state
export const messagesAtom = atom<ChatMessage[]>([]);
export const inputMessageAtom = atom<string>('');
export const sessionsAtom = atom<Session[]>([]);
export const systemPromptAtom = atom<string>('{{.Builtin.SystemPrompt}}');
export const primaryBranchIdAtom = atom<string>('');
export const currentSessionNameAtom = atom<string>(''); // Track current session name (including temporary sessions)

// Derived atoms
export const addMessageAtom = atom(null, (_get, set, newMessage: ChatMessage) => {
  const currentMessages = _get(messagesAtom);
  set(messagesAtom, [...currentMessages, newMessage]);
});

export const updateAgentMessageAtom = atom(
  null,
  (_get, set, payload: { messageId: string; text: string; modelName?: string }) => {
    const { messageId, text: newMessageText } = payload;
    const currentMessages = _get(messagesAtom);
    const existingMessageIndex = currentMessages.findIndex((msg) => msg.id === messageId);

    if (existingMessageIndex !== -1) {
      const newMessages = [...currentMessages];
      const existingMessage = newMessages[existingMessageIndex];
      newMessages[existingMessageIndex] = {
        ...existingMessage,
        parts: [{ text: (existingMessage.parts[0]?.text || '') + newMessageText }],
      };
      set(messagesAtom, newMessages);
    } else {
      const newMessage: ChatMessage = {
        id: messageId,
        parts: [{ text: newMessageText }],
        type: 'model',
        model: payload.modelName,
      };
      set(messagesAtom, [...currentMessages, newMessage]);
    }
  },
);

export const addErrorMessageAtom = atom(null, (_get, set, errorMessageText: string) => {
  const currentMessages = _get(messagesAtom);
  const newMessages = [...currentMessages];
  const lastMessage = newMessages[newMessages.length - 1];

  if (lastMessage && lastMessage.type === 'model' && lastMessage.parts[0]?.text === '') {
    newMessages.pop();
  }

  const errorMessage: ChatMessage = {
    id: crypto.randomUUID(),
    parts: [{ text: errorMessageText }],
    type: 'model_error',
  };
  newMessages.push(errorMessage);
  set(messagesAtom, newMessages);
});

export const resetChatSessionStateAtom = atom(null, (_get, set) => {
  set(messagesAtom, []);
  set(systemPromptAtom, '');
  set(primaryBranchIdAtom, '');
  set(currentSessionNameAtom, '');
  // Note: selectedFilesAtom is NOT reset - attachments are preserved across sessions
  // Note: isSystemPromptEditingAtom is NOT reset here - handled separately
});

export const addSessionAtom = atom(null, (_get, set, newSession: Session) => {
  const currentSessions = _get(sessionsAtom);
  set(sessionsAtom, [newSession, ...currentSessions]);
});

export const setSessionNameAtom = atom(null, (_get, set, payload: { sessionId: string; name: string }) => {
  if (payload.sessionId.startsWith('.')) {
    return;
  }

  const currentSessions = _get(sessionsAtom);
  if (currentSessions.find((s) => s.id === payload.sessionId)) {
    set(
      sessionsAtom,
      currentSessions.map((session) =>
        session.id === payload.sessionId ? { ...session, name: payload.name } : session,
      ),
    );
  } else {
    set(sessionsAtom, [
      { id: payload.sessionId, name: payload.name, last_updated_at: new Date().toISOString() },
      ...currentSessions,
    ]);
  }
});

export const updateUserMessageIdAtom = atom(null, (_get, set, payload: { temporaryId: string; newId: string }) => {
  const { temporaryId, newId } = payload;
  const currentMessages = _get(messagesAtom);
  set(
    messagesAtom,
    currentMessages.map((message) => (message.id === temporaryId ? { ...message, id: newId } : message)),
  );
});

export const updateMessageTokenCountAtom = atom(
  null,
  (_get, set, payload: { messageId: string; cumulTokenCount: number }) => {
    const { messageId, cumulTokenCount } = payload;
    const currentMessages = _get(messagesAtom);
    set(
      messagesAtom,
      currentMessages.map((message) =>
        message.id === messageId ? { ...message, cumulTokenCount: cumulTokenCount } : message,
      ),
    );
  },
);
