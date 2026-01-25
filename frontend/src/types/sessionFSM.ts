import type { FileAttachment } from '../types/chat';
import type { ModelInfo } from '../api/models';

// Message expansion state for managing pagination/loading
export type MessageExpansionState =
  | { type: 'initial' }
  | { type: 'loading_earlier' }
  | { type: 'ready'; hasEarlier: boolean };

// Active operation type for tracking current API operation
export type ActiveOperation = 'none' | 'loading' | 'sending' | 'streaming';

// FSM State Types - handles session identification and related boolean/enumeration states
export type SessionState =
  | { status: 'no_session'; workspaceId: string | undefined; activeOperation: ActiveOperation }
  | {
      status: 'session_loading';
      sessionId: string;
      workspaceId?: string;
      messageState: MessageExpansionState;
      activeOperation: ActiveOperation;
    }
  | {
      status: 'session_ready';
      sessionId: string;
      workspaceId?: string;
      messageState: MessageExpansionState;
      isStreaming: boolean;
      activeOperation: ActiveOperation;
    }
  | {
      status: 'session_ready';
      sessionId: string;
      workspaceId?: string;
      messageState: MessageExpansionState;
      isStreaming: boolean;
      activeOperation: ActiveOperation;
      startTime: number; // Only present when streaming
    }
  | { status: 'session_error'; error: string; workspaceId?: string; activeOperation: ActiveOperation };

// URL Path Types
export type URLPath =
  | { type: 'new_session'; workspaceId: string; isTemporary: boolean }
  | { type: 'existing_session'; sessionId: string }
  | { type: 'session_list'; workspaceId: string };

// FSM Action Types - handles session identification and related state changes
export type SessionAction =
  | { type: 'URL_CHANGED'; urlPath: URLPath }
  | { type: 'LOAD_SESSION'; sessionId: string; workspaceId?: string } // Direct API call trigger
  | {
      type: 'SEND_MESSAGE';
      content: string;
      attachments: FileAttachment[];
      model: ModelInfo | null;
      systemPrompt?: string;
      workspaceId?: string;
      primaryBranchId?: string;
      initialRoots?: string[];
      beforeMessageId?: string;
      isTemporary?: boolean;
    } // Direct API call trigger
  | { type: 'SWITCH_BRANCH'; branchId: string } // Direct API call trigger
  | { type: 'CONFIRM_TOOL'; branchId: string } // Direct API call trigger
  | { type: 'LOAD_EARLIER_MESSAGES' } // Direct API call trigger
  | { type: 'SESSION_LOADING'; sessionId: string; workspaceId?: string; activeOperation: ActiveOperation }
  | {
      type: 'SESSION_LOADED';
      sessionId: string;
      workspaceId?: string;
      hasEarlier?: boolean;
      activeOperation: ActiveOperation;
    }
  | { type: 'SESSION_CREATED'; sessionId: string; workspaceId?: string }
  | { type: 'STREAM_STARTED'; activeOperation: ActiveOperation }
  | { type: 'STREAM_COMPLETED'; activeOperation: ActiveOperation }
  | { type: 'OPERATION_STARTED'; operation: ActiveOperation }
  | { type: 'OPERATION_COMPLETED'; activeOperation: ActiveOperation }
  | { type: 'OPERATION_FAILED'; error: string; activeOperation: ActiveOperation }
  | { type: 'EARLIER_MESSAGES_LOADING' }
  | { type: 'EARLIER_MESSAGES_LOADED'; hasMore: boolean }
  | { type: 'ERROR_OCCURRED'; error: string }
  | { type: 'RESET_SESSION' }
  | { type: 'WORKSPACE_ID_HINT'; workspaceId: string };
