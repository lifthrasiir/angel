import type { SessionState } from '../types/sessionFSM';

/**
 * Helper functions to read values from SessionState
 * These can be used directly in components instead of a bridge
 */

export const getSessionId = (state: SessionState): string | null => {
  switch (state.status) {
    case 'session_loading':
    case 'session_ready':
      return state.sessionId;
    default:
      return null;
  }
};

export const getWorkspaceId = (state: SessionState): string | undefined => {
  return state.workspaceId;
};

export const isLoading = (state: SessionState): boolean => {
  return (
    state.status === 'session_loading' ||
    (state.status === 'session_ready' && state.messageState.type === 'loading_earlier')
  );
};

export const isStreaming = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.isStreaming;
};

export const hasMoreMessages = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.messageState.type === 'ready' && state.messageState.hasEarlier;
};

export const canLoadEarlier = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.messageState.type === 'ready' && state.messageState.hasEarlier;
};

export const isLoadComplete = (state: SessionState): boolean => {
  return state.status === 'session_ready' && state.messageState.type === 'ready';
};

export const getError = (state: SessionState): string | null => {
  return state.status === 'session_error' ? state.error : null;
};

/**
 * Check if session state represents an existing session (vs new session)
 */
export const isExistingSession = (state: SessionState): boolean => {
  return state.status === 'session_loading' || state.status === 'session_ready';
};

/**
 * Check if session state represents a new session
 */
export const isNewSession = (state: SessionState): boolean => {
  return state.status === 'no_session';
};
