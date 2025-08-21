import { atom } from 'jotai';
import type { ChatMessage, Session } from '../types/chat';
import { ModelInfo } from '../api/models';
import { PredefinedPrompt } from '../components/SystemPromptEditor';

export const userEmailAtom = atom<string | null>(null);
export const chatSessionIdAtom = atom<string | null>(null);
export const messagesAtom = atom<ChatMessage[]>([]);
export const inputMessageAtom = atom<string>('');
export const sessionsAtom = atom<Session[]>([]);
export const lastAutoDisplayedThoughtIdAtom = atom<string | null>(null);
export const processingStartTimeAtom = atom<number | null>(null);
export const statusMessageAtom = atom<string | null>(null); // New atom for status messages
export const systemPromptAtom = atom<string>('{{.Builtin.SystemPrompt}}');
export const isSystemPromptEditingAtom = atom<boolean>(false);
export const selectedFilesAtom = atom<File[]>([]);
export const workspaceIdAtom = atom<string | undefined>(undefined);
export const workspaceNameAtom = atom<string | undefined>(undefined);
export const primaryBranchIdAtom = atom<string>('');
export const availableModelsAtom = atom<Map<string, ModelInfo>>(new Map());
export const selectedModelAtom = atom<ModelInfo | null>(null);
export const globalPromptsAtom = atom<PredefinedPrompt[]>([]);
export const selectedGlobalPromptAtom = atom<string>('');
export const isPickingDirectoryAtom = atom<boolean>(false);

// Derived atom for adding messages
export const addMessageAtom = atom(
  null, // This is a write-only atom, so the first argument is null
  (_get, set, newMessage: ChatMessage) => {
    const currentMessages = _get(messagesAtom);
    set(messagesAtom, [...currentMessages, newMessage]);
  },
);

// Derived atom for updating agent messages
export const updateAgentMessageAtom = atom(null, (_get, set, payload: { messageId: string; text: string }) => {
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
      role: 'model',
      parts: [{ text: newMessageText }],
      type: 'model',
    };
    set(messagesAtom, [...currentMessages, newMessage]);
  }
});

// Derived atom for adding error messages
export const addErrorMessageAtom = atom(null, (_get, set, errorMessageText: string) => {
  const currentMessages = _get(messagesAtom);
  const newMessages = [...currentMessages];
  const lastMessage = newMessages[newMessages.length - 1];

  if (lastMessage && lastMessage.type === 'model' && lastMessage.parts[0]?.text === '') {
    newMessages.pop();
  }

  const errorMessage: ChatMessage = {
    id: crypto.randomUUID(),
    role: 'model',
    parts: [{ text: errorMessageText }],
    type: 'model_error',
  };
  newMessages.push(errorMessage);
  set(messagesAtom, newMessages);
});

// Derived atom for resetting chat session state
export const resetChatSessionStateAtom = atom(null, (_get, set) => {
  set(chatSessionIdAtom, null);
  set(messagesAtom, []);
  set(systemPromptAtom, '');
  set(isSystemPromptEditingAtom, true);
  set(selectedFilesAtom, []);
  set(primaryBranchIdAtom, '');
  set(selectedModelAtom, null); // Add this line
});

// Derived atom for setting session name
export const setSessionNameAtom = atom(null, (_get, set, payload: { sessionId: string; name: string }) => {
  const currentSessions = _get(sessionsAtom);
  set(
    sessionsAtom,
    currentSessions.map((session) => (session.id === payload.sessionId ? { ...session, name: payload.name } : session)),
  );
});

// Derived atom for updating user message ID
export const updateUserMessageIdAtom = atom(null, (_get, set, payload: { temporaryId: string; newId: string }) => {
  const { temporaryId, newId } = payload;
  const currentMessages = _get(messagesAtom);
  set(
    messagesAtom,
    currentMessages.map((message) => (message.id === temporaryId ? { ...message, id: newId } : message)),
  );
});

// Derived atom for updating message token count
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
