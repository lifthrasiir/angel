import { useSessionManagerContext } from './SessionManagerContext';

/**
 * Custom hook to get processing state from SessionState
 * Eliminates prop drilling by providing global access to processing information
 */
export const useProcessingState = () => {
  const sessionManager = useSessionManagerContext();

  const isProcessing =
    sessionManager.sessionState.activeOperation !== 'none' && sessionManager.sessionState.activeOperation !== 'loading';

  const startTime =
    sessionManager.sessionState.status === 'session_ready' && 'startTime' in sessionManager.sessionState
      ? sessionManager.sessionState.startTime
      : null;

  const isStreaming = sessionManager.sessionState.status === 'session_ready' && sessionManager.sessionState.isStreaming;

  return {
    isProcessing,
    startTime,
    isStreaming,
  };
};
