// Message expansion state for managing pagination/loading
export type MessageExpansionState =
  | { type: 'initial' }
  | { type: 'loading_earlier' }
  | { type: 'ready'; hasEarlier: boolean };

// FSM State Types - handles session identification and related boolean/enumeration states
export type SessionState =
  | { status: 'no_session'; workspaceId: string | undefined }
  | { status: 'session_loading'; sessionId: string; workspaceId?: string; messageState: MessageExpansionState }
  | {
      status: 'session_ready';
      sessionId: string;
      workspaceId?: string;
      messageState: MessageExpansionState;
      isStreaming: boolean;
    }
  | { status: 'session_error'; error: string; workspaceId?: string };

// URL Path Types
export type URLPath =
  | { type: 'new_global' }
  | { type: 'new_workspace'; workspaceId: string }
  | { type: 'existing_session'; sessionId: string };

// FSM Action Types - handles session identification and related state changes
export type SessionAction =
  | { type: 'URL_CHANGED'; urlPath: URLPath }
  | { type: 'SESSION_LOADING'; sessionId: string; workspaceId?: string }
  | { type: 'SESSION_LOADED'; sessionId: string; workspaceId?: string; hasEarlier?: boolean }
  | { type: 'STREAM_STARTED' }
  | { type: 'STREAM_COMPLETED' }
  | { type: 'EARLIER_MESSAGES_LOADING' }
  | { type: 'EARLIER_MESSAGES_LOADED'; hasMore: boolean }
  | { type: 'ERROR_OCCURRED'; error: string }
  | { type: 'RESET_SESSION' }
  | { type: 'WORKSPACE_ID_HINT'; workspaceId: string };

// SSE Event Types (from server) - Import from events.ts
export type SseModelMessage = {
  type: 'M';
  messageId: string;
  modelName?: string;
  text?: string;
  cumulTokenCount?: number;
};
