import { useReducer, useCallback, useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { sessionReducer } from '../reducers/sessionReducer';
import { parseURLPath } from '../utils/urlSessionMapping';
import {
  getSessionId,
  getWorkspaceId,
  isLoading,
  isStreaming,
  hasMoreMessages,
  canLoadEarlier,
  getError,
} from '../utils/sessionStateHelpers';

export interface SessionManager {
  // FSM state
  sessionState: any;

  // Computed values
  sessionId: string | null;
  workspaceId: string | null;
  isLoading: boolean;
  isStreaming: boolean;
  hasMoreMessages: boolean;
  canLoadEarlier: boolean;
  error: string | null;

  // Actions
  startSessionLoading: (sessionId: string, workspaceId?: string) => void;
  completeSessionLoading: (sessionId: string, workspaceId?: string, hasMore?: boolean) => void;
  startStreaming: () => void;
  completeStreaming: () => void;
  startLoadingEarlier: () => void;
  completeLoadingEarlier: (hasMore: boolean) => void;
  handleError: (error: string) => void;
  resetSession: () => void;
  navigateToNewSession: (workspaceId?: string) => void;
  navigateToSession: (sessionId: string) => void;
  setSessionWorkspaceId: (workspaceId: string) => void;

  // Global stream management
  closeAllStreams: () => void;
  setActiveEventSource: (es: EventSource | null) => void;
  setActiveAbortController: (ac: AbortController | null) => void;
}

// Global stream registry for centralized stream management
const globalStreamRegistry = {
  activeEventSource: null as EventSource | null,
  activeAbortController: null as AbortController | null,

  setEventSource: function (es: EventSource | null) {
    if (
      this.activeEventSource &&
      this.activeEventSource !== es &&
      this.activeEventSource.readyState !== EventSource.CLOSED
    ) {
      this.activeEventSource.close();
    }
    this.activeEventSource = es;
  },

  setAbortController: function (ac: AbortController | null) {
    if (this.activeAbortController && this.activeAbortController !== ac && !this.activeAbortController.signal.aborted) {
      this.activeAbortController.abort();
    }
    this.activeAbortController = ac;
  },

  closeAllStreams: function () {
    // Close active EventSource
    if (this.activeEventSource) {
      if (this.activeEventSource.readyState !== EventSource.CLOSED) {
        this.activeEventSource.close();
      }
      this.activeEventSource = null;
    }

    // Abort active fetch stream
    if (this.activeAbortController) {
      if (!this.activeAbortController.signal.aborted) {
        this.activeAbortController.abort();
      }
      this.activeAbortController = null;
    }
  },
};

export const useSessionManager = () => {
  const location = useLocation();
  const navigate = useNavigate();

  // FSM state management - handles session identification and related states
  const [sessionState, dispatch] = useReducer(sessionReducer, {
    status: 'no_session',
    workspaceId: undefined,
  });

  // Handle URL changes
  useEffect(() => {
    const urlPath = parseURLPath(location.pathname);
    dispatch({ type: 'URL_CHANGED', urlPath });
  }, [location.pathname]);

  // Start session loading
  const startSessionLoading = useCallback((sessionId: string, workspaceId?: string) => {
    dispatch({ type: 'SESSION_LOADING', sessionId, workspaceId });
  }, []);

  // Complete session loading
  const completeSessionLoading = useCallback((sessionId: string, workspaceId?: string, hasEarlier?: boolean) => {
    dispatch({ type: 'SESSION_LOADED', sessionId, workspaceId, hasEarlier });
  }, []);

  // Start streaming
  const startStreaming = useCallback(() => {
    dispatch({ type: 'STREAM_STARTED' });
  }, []);

  // Complete streaming
  const completeStreaming = useCallback(() => {
    dispatch({ type: 'STREAM_COMPLETED' });
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

    // Actions
    startSessionLoading,
    completeSessionLoading,
    startStreaming,
    completeStreaming,
    startLoadingEarlier,
    completeLoadingEarlier,
    handleError,
    resetSession,
    navigateToNewSession,
    navigateToSession,
    setSessionWorkspaceId,

    // Global stream management
    closeAllStreams: globalStreamRegistry.closeAllStreams.bind(globalStreamRegistry),
    setActiveEventSource: globalStreamRegistry.setEventSource.bind(globalStreamRegistry),
    setActiveAbortController: globalStreamRegistry.setAbortController.bind(globalStreamRegistry),
  } as SessionManager;
};
