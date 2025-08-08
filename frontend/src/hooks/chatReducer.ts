import type { ChatMessage, Session } from '../types/chat';

// Action Types
export const SET_USER_EMAIL = 'SET_USER_EMAIL';
export const SET_CHAT_SESSION_ID = 'SET_CHAT_SESSION_ID';
export const SET_MESSAGES = 'SET_MESSAGES';
export const SET_INPUT_MESSAGE = 'SET_INPUT_MESSAGE';
export const SET_SESSIONS = 'SET_SESSIONS';
export const SET_LAST_AUTO_DISPLAYED_THOUGHT_ID = 'SET_LAST_AUTO_DISPLAYED_THOUGHT_ID';
export const SET_IS_STREAMING = 'SET_IS_STREAMING';
export const SET_SYSTEM_PROMPT = 'SET_SYSTEM_PROMPT';
export const SET_IS_SYSTEM_PROMPT_EDITING = 'SET_IS_SYSTEM_PROMPT_EDITING';
export const SET_SELECTED_FILES = 'SET_SELECTED_FILES';
export const ADD_MESSAGE = 'ADD_MESSAGE';
export const UPDATE_AGENT_MESSAGE = 'UPDATE_AGENT_MESSAGE';
export const RESET_CHAT_SESSION_STATE = 'RESET_CHAT_SESSION_STATE';
export const ADD_ERROR_MESSAGE = 'ADD_ERROR_MESSAGE'; // New Action Type
export const SET_SESSION_NAME = 'SET_SESSION_NAME';
export const SET_WORKSPACE_ID = 'SET_WORKSPACE_ID'; // New Action Type
export const SET_WORKSPACE_NAME = 'SET_WORKSPACE_NAME'; // New Action Type
export const SET_PRIMARY_BRANCH_ID = 'SET_PRIMARY_BRANCH_ID';
export const UPDATE_USER_MESSAGE_ID = 'UPDATE_USER_MESSAGE_ID';
export const UPDATE_MESSAGE_TOKEN_COUNT = 'UPDATE_MESSAGE_TOKEN_COUNT';

// State Interface
export interface ChatState {
  userEmail: string | null;
  chatSessionId: string | null;
  messages: ChatMessage[];
  inputMessage: string;
  sessions: Session[];
  lastAutoDisplayedThoughtId: string | null;
  isStreaming: boolean;
  systemPrompt: string;
  isSystemPromptEditing: boolean;
  selectedFiles: File[];
  workspaceId?: string; // Added workspaceId to state
  workspaceName?: string; // Added workspaceName to state
  primaryBranchId: string; // New field for primary branch ID
}

// Initial State
export const initialState: ChatState = {
  userEmail: null,
  chatSessionId: null,
  messages: [],
  inputMessage: '',
  sessions: [],
  lastAutoDisplayedThoughtId: null,
  isStreaming: false,
  systemPrompt: '{{.Builtin.SystemPrompt}}',
  isSystemPromptEditing: false,
  selectedFiles: [],
  workspaceId: undefined, // Added workspaceId to initial state
  workspaceName: undefined, // Added workspaceName to initial state
  primaryBranchId: '', // Initialize primaryBranchId
};

// Action Interface
export type ChatAction =
  | { type: typeof SET_USER_EMAIL; payload: string | null }
  | { type: typeof SET_CHAT_SESSION_ID; payload: string | null }
  | { type: typeof SET_MESSAGES; payload: ChatMessage[] }
  | { type: typeof SET_INPUT_MESSAGE; payload: string }
  | { type: typeof SET_SESSIONS; payload: Session[] }
  | { type: typeof SET_LAST_AUTO_DISPLAYED_THOUGHT_ID; payload: string | null }
  | { type: typeof SET_IS_STREAMING; payload: boolean }
  | { type: typeof SET_SYSTEM_PROMPT; payload: string }
  | { type: typeof SET_IS_SYSTEM_PROMPT_EDITING; payload: boolean }
  | { type: typeof SET_SELECTED_FILES; payload: File[] }
  | { type: typeof ADD_MESSAGE; payload: ChatMessage }
  | { type: typeof UPDATE_AGENT_MESSAGE; payload: { messageId: string; text: string } }
  | { type: typeof RESET_CHAT_SESSION_STATE }
  | { type: typeof ADD_ERROR_MESSAGE; payload: string }
  | {
      type: typeof SET_SESSION_NAME;
      payload: { sessionId: string; name: string };
    }
  | { type: typeof SET_WORKSPACE_ID; payload: string | undefined }
  | { type: typeof SET_WORKSPACE_NAME; payload: string | undefined }
  | { type: typeof SET_PRIMARY_BRANCH_ID; payload: string }
  | { type: typeof UPDATE_USER_MESSAGE_ID; payload: { temporaryId: string; newId: string } }
  | { type: typeof UPDATE_MESSAGE_TOKEN_COUNT; payload: { messageId: string; cumulTokenCount: number } };

// Reducer Function
export const chatReducer = (state: ChatState, action: ChatAction): ChatState => {
  switch (action.type) {
    case SET_USER_EMAIL:
      return { ...state, userEmail: action.payload };
    case SET_CHAT_SESSION_ID:
      return { ...state, chatSessionId: action.payload };
    case SET_MESSAGES:
      return { ...state, messages: action.payload };
    case SET_INPUT_MESSAGE:
      return { ...state, inputMessage: action.payload };
    case SET_SESSIONS:
      return { ...state, sessions: action.payload };
    case SET_LAST_AUTO_DISPLAYED_THOUGHT_ID:
      return { ...state, lastAutoDisplayedThoughtId: action.payload };
    case SET_IS_STREAMING:
      return { ...state, isStreaming: action.payload };
    case SET_SYSTEM_PROMPT:
      return { ...state, systemPrompt: action.payload };
    case SET_IS_SYSTEM_PROMPT_EDITING:
      return { ...state, isSystemPromptEditing: action.payload };
    case SET_SELECTED_FILES:
      return { ...state, selectedFiles: action.payload };
    case ADD_MESSAGE: {
      const newMessage = action.payload;
      const lastMessage = state.messages[state.messages.length - 1];

      if (
        newMessage.type === 'thought' &&
        lastMessage &&
        lastMessage.type === 'model' &&
        lastMessage.parts[0]?.text === ''
      ) {
        // Insert the thought message before the empty model message
        const newMessages = [...state.messages];
        newMessages.splice(newMessages.length - 1, 0, newMessage);
        return { ...state, messages: newMessages };
      } else {
        // Otherwise, just add the message to the end
        return { ...state, messages: [...state.messages, newMessage] };
      }
    }
    case UPDATE_AGENT_MESSAGE: {
      const { messageId, text: newMessageText } = action.payload;
      const existingMessageIndex = state.messages.findIndex((msg) => msg.id === messageId);

      if (existingMessageIndex !== -1) {
        // Update existing message
        const newMessages = [...state.messages];
        const existingMessage = newMessages[existingMessageIndex];
        newMessages[existingMessageIndex] = {
          ...existingMessage,
          parts: [{ text: (existingMessage.parts[0]?.text || '') + newMessageText }],
        };
        return { ...state, messages: newMessages };
      } else {
        // Add new message
        const newMessage: ChatMessage = {
          id: messageId,
          role: 'model',
          parts: [{ text: newMessageText }],
          type: 'model',
        };
        return { ...state, messages: [...state.messages, newMessage] };
      }
    }
    case ADD_ERROR_MESSAGE: {
      const errorMessageText = action.payload;
      const newMessages = [...state.messages];
      const lastMessage = newMessages[newMessages.length - 1];

      // Check if the last message is an empty model message and remove it
      if (lastMessage && lastMessage.type === 'model' && lastMessage.parts[0]?.text === '') {
        newMessages.pop();
      }

      // Add the new error message
      const errorMessage: ChatMessage = {
        id: crypto.randomUUID(),
        role: 'model', // Or 'system' if preferred for errors
        parts: [{ text: errorMessageText }],
        type: 'model_error', // Custom type for error messages
      };
      newMessages.push(errorMessage);
      return { ...state, messages: newMessages };
    }
    case RESET_CHAT_SESSION_STATE:
      return {
        ...state,
        chatSessionId: null,
        messages: [],
        systemPrompt: '',
        isSystemPromptEditing: true,
        selectedFiles: [],
        primaryBranchId: '',
      };
    case SET_SESSION_NAME:
      return {
        ...state,
        sessions: state.sessions.map((session) =>
          session.id === action.payload.sessionId ? { ...session, name: action.payload.name } : session,
        ),
      };
    case SET_WORKSPACE_ID:
      return { ...state, workspaceId: action.payload };
    case SET_WORKSPACE_NAME:
      return { ...state, workspaceName: action.payload };
    case SET_PRIMARY_BRANCH_ID:
      return { ...state, primaryBranchId: action.payload };
    case UPDATE_USER_MESSAGE_ID: {
      const { temporaryId, newId } = action.payload;
      return {
        ...state,
        messages: state.messages.map((message) => (message.id === temporaryId ? { ...message, id: newId } : message)),
      };
    }
    case UPDATE_MESSAGE_TOKEN_COUNT: {
      const { messageId, cumulTokenCount } = action.payload;
      return {
        ...state,
        messages: state.messages.map((message) =>
          message.id === messageId ? { ...message, cumulTokenCount: cumulTokenCount } : message,
        ),
      };
    }
    default:
      return state;
  }
};
