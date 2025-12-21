import { useReducer, useCallback, useEffect, useMemo } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { sessionReducer } from '../reducers/sessionReducer';
import { parseURLPath } from '../utils/urlSessionMapping';
import { SessionOperationManager } from '../managers/SessionOperationManager';
import {
  getSessionId,
  getWorkspaceId,
  isLoading,
  isStreaming,
  hasMoreMessages,
  canLoadEarlier,
  getError,
} from '../utils/sessionStateHelpers';
import type { SessionState, SessionAction } from '../types/sessionFSM';

export interface SessionManager {
  // FSM state
  sessionState: SessionState;

  // Computed values
  sessionId: string | null;
  workspaceId: string | null;
  isLoading: boolean;
  isStreaming: boolean;
  hasMoreMessages: boolean;
  canLoadEarlier: boolean;
  error: string | null;
  activeOperation: 'none' | 'loading' | 'sending' | 'streaming';

  // Actions
  dispatch: (action: SessionAction) => void;

  // Legacy actions for backward compatibility
  startSessionLoading: (sessionId: string, workspaceId?: string) => void;
  completeSessionLoading: (sessionId: string, workspaceId?: string, hasMore?: boolean) => void;
  startStreaming: () => void;
  completeStreaming: () => void;
  startLoadingEarlier: () => void;
  completeLoadingEarlier: (hasMore: boolean) => void;
  handleError: (error: string) => void;
  resetSession: () => void;
  navigateToNewSession: (workspaceId?: string) => void;
  navigateToTemporarySession: (workspaceId?: string) => void;
  navigateToSession: (sessionId: string) => void;
  setSessionWorkspaceId: (workspaceId: string) => void;

  // Operation manager access
  operationManager: SessionOperationManager;
}

export const useSessionManager = () => {
  const location = useLocation();
  const navigate = useNavigate();

  // FSM state management - handles session identification and related states
  const [sessionState, dispatch] = useReducer(sessionReducer, {
    status: 'no_session',
    workspaceId: undefined,
    activeOperation: 'none',
  });

  // Create operation manager instance
  const operationManager = useMemo(
    () =>
      new SessionOperationManager({
        dispatch,
        sessionState,
      }),
    [],
  );

  // Handle URL changes
  useEffect(() => {
    const urlPath = parseURLPath(location.pathname);
    dispatch({ type: 'URL_CHANGED', urlPath });
  }, [location.pathname]);

  // Start session loading
  const startSessionLoading = useCallback((sessionId: string, workspaceId?: string) => {
    dispatch({ type: 'SESSION_LOADING', sessionId, workspaceId, activeOperation: 'loading' });
  }, []);

  // Complete session loading
  const completeSessionLoading = useCallback((sessionId: string, workspaceId?: string, hasEarlier?: boolean) => {
    dispatch({ type: 'SESSION_LOADED', sessionId, workspaceId, hasEarlier, activeOperation: 'none' });
  }, []);

  // Start streaming
  const startStreaming = useCallback(() => {
    dispatch({ type: 'STREAM_STARTED', activeOperation: 'streaming' });
  }, []);

  // Complete streaming
  const completeStreaming = useCallback(() => {
    dispatch({ type: 'STREAM_COMPLETED', activeOperation: 'none' });
  }, []);

  // Start loading earlier messages
  const startLoadingEarlier = useCallback(() => {
    dispatch({ type: 'EARLIER_MESSAGES_LOADING' });
  }, []);

  // Complete loading earlier messages
  const completeLoadingEarlier = useCallback((hasMore: boolean) => {
    dispatch({ type: 'EARLIER_MESSAGES_LOADED', hasMore });
  }, []);

  // Handle error
  const handleError = useCallback((error: string) => {
    dispatch({ type: 'ERROR_OCCURRED', error });
  }, []);

  // Reset session
  const resetSession = useCallback(() => {
    dispatch({ type: 'RESET_SESSION' });
  }, []);

  // Navigate to new session
  const navigateToNewSession = useCallback(
    (workspaceId?: string) => {
      const url = workspaceId ? `/w/${workspaceId}/new` : '/new';
      navigate(url);
    },
    [navigate],
  );

  // Navigate to temporary session
  const navigateToTemporarySession = useCallback(
    (workspaceId?: string) => {
      const url = workspaceId ? `/w/${workspaceId}/temp` : '/temp';
      navigate(url);
    },
    [navigate],
  );

  // Navigate to existing session
  const navigateToSession = useCallback(
    (sessionId: string) => {
      navigate(`/${sessionId}`);
    },
    [navigate],
  );

  // Set session workspace ID
  const setSessionWorkspaceId = useCallback((workspaceId: string) => {
    dispatch({ type: 'WORKSPACE_ID_HINT', workspaceId });
  }, []);

  // Get computed values using helper functions
  const sessionId = getSessionId(sessionState);
  const workspaceId = getWorkspaceId(sessionState);
  const isLoadingState = isLoading(sessionState);
  const isStreamingState = isStreaming(sessionState);
  const hasMoreMessagesState = hasMoreMessages(sessionState);
  const canLoadEarlierState = canLoadEarlier(sessionState);
  const errorState = getError(sessionState);
  const activeOperation =
    sessionState.status === 'session_ready' ||
    sessionState.status === 'session_loading' ||
    sessionState.status === 'session_error'
      ? sessionState.activeOperation
      : 'none';

  return {
    // FSM state
    sessionState,

    // Computed values
    sessionId,
    workspaceId,
    isLoading: isLoadingState,
    isStreaming: isStreamingState,
    hasMoreMessages: hasMoreMessagesState,
    canLoadEarlier: canLoadEarlierState,
    error: errorState,
    activeOperation,

    // Actions
    dispatch,

    // Legacy actions for backward compatibility
    startSessionLoading,
    completeSessionLoading,
    startStreaming,
    completeStreaming,
    startLoadingEarlier,
    completeLoadingEarlier,
    handleError,
    resetSession,
    navigateToNewSession,
    navigateToTemporarySession,
    navigateToSession,
    setSessionWorkspaceId,

    // Operation manager access
    operationManager,
  } as SessionManager;
};
