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
        case 'new_session':
          return {
            status: 'no_session',
            workspaceId: urlPath.workspaceId || undefined,
            activeOperation: 'none',
          };

        case 'existing_session':
          // Check if this is the same session we're already on (idempotent)
          if (state.status === 'session_ready' && state.sessionId === urlPath.sessionId) {
            // Same session, don't change state
            return state;
          }
          if (state.status === 'session_loading' && state.sessionId === urlPath.sessionId) {
            // Already loading this session, don't change state
            return state;
          }
          return {
            status: 'session_loading',
            sessionId: urlPath.sessionId,
            workspaceId: undefined, // Will be set when session loads
            messageState: createInitialMessageState(),
            activeOperation: 'loading',
          };

        default:
          return state;
      }
    }

    case 'LOAD_SESSION': {
      // This action triggers the operation manager directly
      return {
        status: 'session_loading',
        sessionId: action.sessionId,
        workspaceId: action.workspaceId,
        messageState: createInitialMessageState(),
        activeOperation: 'loading',
      };
    }

    case 'SESSION_LOADING': {
      return {
        status: 'session_loading',
        sessionId: action.sessionId,
        workspaceId: action.workspaceId,
        messageState: createInitialMessageState(),
        activeOperation: action.activeOperation || 'loading',
      };
    }

    case 'SESSION_LOADED': {
      const isActiveStreaming = action.activeOperation === 'streaming' || action.activeOperation === 'sending';

      const readyState = {
        status: 'session_ready' as const,
        sessionId: action.sessionId,
        workspaceId: action.workspaceId,
        messageState: {
          type: 'ready' as const,
          hasEarlier: action.hasEarlier || false,
        },
        isStreaming: isActiveStreaming,
        activeOperation: action.activeOperation || 'none',
      };

      // Add startTime if streaming
      if (isActiveStreaming) {
        return { ...readyState, startTime: performance.now() };
      }
      return readyState;
    }

    case 'SESSION_CREATED': {
      // Update sessionId without changing other state
      // This is used when a new session is created during message send
      if (state.status === 'no_session') {
        // New session created from no_session state
        const isActiveStreaming = state.activeOperation === 'sending' || state.activeOperation === 'streaming';
        const newState = {
          status: 'session_ready',
          sessionId: action.sessionId,
          workspaceId: action.workspaceId || state.workspaceId,
          messageState: { type: 'initial' },
          isStreaming: isActiveStreaming,
          activeOperation: state.activeOperation,
        } as const;
        if (isActiveStreaming) {
          return { ...newState, startTime: performance.now() };
        } else {
          return newState;
        }
      } else if (state.status === 'session_loading') {
        // Session was loading, now we have the sessionId
        return {
          ...state,
          sessionId: action.sessionId,
          workspaceId: action.workspaceId || state.workspaceId,
        };
      } else if (state.status === 'session_ready') {
        // Update sessionId in ready state (e.g., after branch switch creating new session)
        return {
          ...state,
          sessionId: action.sessionId,
          workspaceId: action.workspaceId || state.workspaceId,
        };
      }
      return state;
    }

    case 'STREAM_STARTED': {
      if (state.status === 'session_ready') {
        const newState = {
          ...state,
          isStreaming: true,
          activeOperation: action.activeOperation || 'streaming',
        };
        if ('startTime' in state) {
          return newState;
        } else {
          return { ...newState, startTime: performance.now() };
        }
      }
      if (state.status === 'no_session') {
        return {
          ...state,
          activeOperation: action.activeOperation || 'streaming',
        };
      }
      return state;
    }

    case 'STREAM_COMPLETED': {
      if (state.status === 'session_ready') {
        if ('startTime' in state) {
          // Remove startTime if it exists
          const { startTime, ...stateWithoutStartTime } = state;
          return {
            ...stateWithoutStartTime,
            isStreaming: false,
            activeOperation: action.activeOperation || 'none',
          };
        } else {
          // No startTime to remove
          return {
            ...state,
            isStreaming: false,
            activeOperation: action.activeOperation || 'none',
          };
        }
      }
      return state;
    }

    case 'SEND_MESSAGE': {
      // This action triggers the operation manager directly
      if (state.status === 'session_ready' || state.status === 'no_session') {
        return {
          ...state,
          activeOperation: 'sending',
        };
      }
      return state;
    }

    case 'SWITCH_BRANCH': {
      // This action triggers the operation manager directly
      if (state.status === 'session_ready') {
        return {
          ...state,
          activeOperation: 'sending',
        };
      }
      return state;
    }

    case 'CONFIRM_TOOL': {
      // This action triggers the operation manager directly
      if (state.status === 'session_ready') {
        return {
          ...state,
          activeOperation: 'sending',
        };
      }
      return state;
    }

    case 'LOAD_EARLIER_MESSAGES': {
      // This action triggers the operation manager directly
      if (state.status === 'session_ready') {
        return {
          ...state,
          messageState: {
            type: 'loading_earlier',
          },
          activeOperation: 'loading',
        };
      }
      return state;
    }

    case 'OPERATION_STARTED': {
      return {
        ...state,
        activeOperation: action.operation,
      };
    }

    case 'OPERATION_COMPLETED': {
      return {
        ...state,
        activeOperation: action.activeOperation,
      };
    }

    case 'OPERATION_FAILED': {
      return {
        status: 'session_error',
        error: action.error,
        workspaceId: state.workspaceId,
        activeOperation: action.activeOperation,
      };
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
        activeOperation: 'none',
      };
    }

    case 'RESET_SESSION': {
      return {
        status: 'no_session',
        workspaceId: undefined,
        activeOperation: 'none',
      };
    }

    case 'WORKSPACE_ID_HINT': {
      // Update workspaceId if different (object identity optimization)
      if (state.workspaceId !== action.workspaceId) {
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

export const selectStartTime = (state: SessionState): number | null => {
  return state.status === 'session_ready' && 'startTime' in state ? state.startTime : null;
};
