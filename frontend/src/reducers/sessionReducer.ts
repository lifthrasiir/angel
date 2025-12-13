import type { SessionState, SessionAction } from '../types/sessionFSM';

// Helper function to create initial message expansion state
const createInitialMessageState = () => ({
  type: 'initial' as const,
});

// Main reducer function - handles session identification and related boolean/enumeration states
export const sessionReducer = (state: SessionState, action: SessionAction): SessionState => {
  switch (action.type) {
    case 'URL_CHANGED': {
      const { urlPath } = action;

      switch (urlPath.type) {
        case 'new_global':
          return {
            status: 'no_session',
            workspaceId: undefined,
          };

        case 'new_temp':
          return {
            status: 'no_session',
            workspaceId: undefined,
          };

        case 'new_workspace':
          return {
            status: 'no_session',
            workspaceId: urlPath.workspaceId,
          };

        case 'existing_session':
          return {
            status: 'session_loading',
            sessionId: urlPath.sessionId,
            workspaceId: undefined, // Will be set when session loads
            messageState: createInitialMessageState(),
          };

        default:
          return state;
      }
    }

    case 'SESSION_LOADING': {
      return {
        status: 'session_loading',
        sessionId: action.sessionId,
        workspaceId: action.workspaceId,
        messageState: createInitialMessageState(),
      };
    }

    case 'SESSION_LOADED': {
      return {
        status: 'session_ready' as const,
        sessionId: action.sessionId,
        workspaceId: action.workspaceId,
        messageState: {
          type: 'ready' as const,
          hasEarlier: action.hasEarlier || false,
        },
        isStreaming: false,
      };
    }

    case 'STREAM_STARTED': {
      if (state.status === 'session_ready') {
        return {
          ...state,
          isStreaming: true,
        };
      }
      return state;
    }

    case 'STREAM_COMPLETED': {
      if (state.status === 'session_ready') {
        return {
          ...state,
          isStreaming: false,
        };
      }
      return state;
    }

    case 'EARLIER_MESSAGES_LOADING': {
      if (state.status === 'session_ready') {
        return {
          ...state,
          messageState: {
            type: 'loading_earlier',
          },
        };
      }
      return state;
    }

    case 'EARLIER_MESSAGES_LOADED': {
      if (state.status === 'session_ready') {
        return {
          ...state,
          messageState: {
            type: 'ready',
            hasEarlier: action.hasMore,
          },
        };
      }
      return state;
    }

    case 'ERROR_OCCURRED': {
      return {
        status: 'session_error',
        error: action.error,
        workspaceId: state.workspaceId,
      };
    }

    case 'RESET_SESSION': {
      return {
        status: 'no_session',
        workspaceId: undefined,
      };
    }

    case 'WORKSPACE_ID_HINT': {
      // Only update workspaceId if it's currently undefined or null
      if (state.workspaceId === undefined || state.workspaceId === null) {
        return {
          ...state,
          workspaceId: action.workspaceId,
        };
      }
      return state;
    }

    default:
      return state;
  }
};

// Selector functions for accessing state
export const selectSessionId = (state: SessionState): string | null => {
  switch (state.status) {
    case 'session_loading':
    case 'session_ready':
      return state.sessionId;
    default:
      return null;
  }
};

export const selectWorkspaceId = (state: SessionState): string | undefined => {
  return state.workspaceId;
};

export const selectIsLoading = (state: SessionState): boolean => {
  return (
    state.status === 'session_loading' ||
    (state.status === 'session_ready' && state.messageState.type === 'loading_earlier')
  );
};

export const selectIsStreaming = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.isStreaming;
};

export const selectHasMoreMessages = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.messageState.type === 'ready' && state.messageState.hasEarlier;
};

export const selectCanLoadEarlier = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.messageState.type === 'ready' && state.messageState.hasEarlier;
};

export const selectError = (state: SessionState): string | null => {
  return state.status === 'session_error' ? state.error : null;
};
