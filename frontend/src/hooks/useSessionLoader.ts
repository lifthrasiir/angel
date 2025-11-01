import { useEffect, useRef, useCallback } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useSetAtom, useAtomValue } from 'jotai';
import type { ChatMessage, InitialState } from '../types/chat';
import {
  EventComplete,
  EventError,
  EventFunctionCall,
  EventFunctionResponse,
  EventInitialState,
  EventInitialStateNoCall,
  EventInlineData,
  EventModelMessage,
  EventPing,
  EventThought,
  EventPendingConfirmation,
  EventWorkspaceHint,
} from '../utils/messageHandler';
import { loadSession } from '../utils/sessionManager';
import { splitOnceByNewline } from '../utils/stringUtils';
import { fetchSessionHistory } from '../api/apiClient';
import {
  addErrorMessageAtom,
  addMessageAtom,
  resetChatSessionStateAtom,
  inputMessageAtom,
  processingStartTimeAtom,
  isSystemPromptEditingAtom,
  messagesAtom,
  primaryBranchIdAtom,
  selectedFilesAtom,
  systemPromptAtom,
  updateAgentMessageAtom,
  hasMoreMessagesAtom,
  isPriorSessionLoadCompleteAtom,
  pendingConfirmationAtom,
  temporaryEnvChangeMessageAtom,
  preserveSelectedFilesAtom,
  isModelManuallySelectedAtom,
} from '../atoms/chatAtoms';
import { useScrollAdjustment } from './useScrollAdjustment';
import { isLoading, hasMoreMessages } from '../utils/sessionStateHelpers';
import { SessionManager } from './useSessionManager';

interface UseSessionLoaderProps {
  chatSessionId: string | null;
  chatAreaRef: React.RefObject<HTMLDivElement>;
  sessionManager: SessionManager; // sessionManager is now required for FSM integration
  onSessionSwitch?: () => void; // Callback to notify when session switches
}

const FETCH_LIMIT = 50;

export const useSessionLoader = ({
  chatSessionId,
  chatAreaRef,
  sessionManager,
  onSessionSwitch,
}: UseSessionLoaderProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId, workspaceId: urlWorkspaceId } = useParams<{
    sessionId?: string;
    workspaceId?: string;
  }>();
  const location = useLocation();

  const setInputMessage = useSetAtom(inputMessageAtom);
  const setProcessingStartTime = useSetAtom(processingStartTimeAtom);
  const setIsSystemPromptEditing = useSetAtom(isSystemPromptEditingAtom);
  const setHasMoreMessages = useSetAtom(hasMoreMessagesAtom);
  const setMessages = useSetAtom(messagesAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);
  const setIsPriorSessionLoadComplete = useSetAtom(isPriorSessionLoadCompleteAtom);
  const setPendingConfirmation = useSetAtom(pendingConfirmationAtom);
  const setTemporaryEnvChangeMessage = useSetAtom(temporaryEnvChangeMessageAtom);
  const preserveSelectedFiles = useAtomValue(preserveSelectedFilesAtom);
  const setPreserveSelectedFiles = useSetAtom(preserveSelectedFilesAtom);
  const setIsModelManuallySelected = useSetAtom(isModelManuallySelectedAtom);
  const messages = useAtomValue(messagesAtom);
  const processingStartTime = useAtomValue(processingStartTimeAtom);

  const eventSourceRef = useRef<EventSource | null>(null);
  const isStreamEndedNormallyRef = useRef(false);
  const isLoadingMoreRef = useRef(false);
  const latestSessionIdRef = useRef<string | null>(null);
  const sessionLoadAbortControllerRef = useRef<AbortController | null>(null);
  const activeEventSourceRef = useRef<EventSource | null>(null);

  const { adjustScroll } = useScrollAdjustment({ chatAreaRef });

  const closeEventSourceNormally = () => {
    // Force close ALL EventSources and clear all references first
    // This prevents any race conditions during rapid session switching
    if (eventSourceRef.current) {
      isStreamEndedNormallyRef.current = true;
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }

    // Clear the active reference to prevent any handler execution
    activeEventSourceRef.current = null;

    // Abort session loading if in progress
    if (sessionLoadAbortControllerRef.current) {
      sessionLoadAbortControllerRef.current.abort();
      sessionLoadAbortControllerRef.current = null;
    }

    // Close ALL streams globally before notifying about session switch
    sessionManager.closeAllStreams();

    // Notify about session switch to cancel message streams
    if (onSessionSwitch) {
      onSessionSwitch();
    }

    // Reset processing state when closing EventSource
    setProcessingStartTime(null);
  };

  const mapToChatMessages = (rawMessages: any[]): ChatMessage[] => {
    return rawMessages.map((msg: any) => {
      const chatMessage: ChatMessage = {
        ...msg,
        id: msg.id,
        attachments: msg.attachments,
        cumulTokenCount: msg.cumul_token_count,
        sessionId: latestSessionIdRef.current || urlSessionId,
      };
      if (msg.type === 'thought') {
        chatMessage.type = 'thought';
      } else if (msg.type === 'model_error') {
        chatMessage.type = 'model_error';
      } else if (msg.parts?.[0]?.functionCall) {
        chatMessage.type = 'function_call';
        chatMessage.parts[0] = {
          functionCall: msg.parts[0].functionCall,
        };
      } else if (msg.parts?.[0]?.functionResponse) {
        chatMessage.type = 'function_response';
        chatMessage.parts[0] = {
          functionResponse: msg.parts[0].functionResponse,
        };
      } else {
        chatMessage.type = msg.type;
      }
      return chatMessage;
    });
  };

  const loadMoreMessages = useCallback(async () => {
    // Prevent duplicate calls
    if (isLoadingMoreRef.current) {
      return;
    }

    // Use FSM state for loading status
    const isPriorSessionLoadingFromFS = isLoading(sessionManager.sessionState);
    const hasMoreMessagesFromFS = hasMoreMessages(sessionManager.sessionState);

    // Don't load more messages while loading is in progress (but allow during streaming)
    if (messages.length === 0 || isPriorSessionLoadingFromFS || !hasMoreMessagesFromFS) {
      return;
    }

    const firstMessageId = messages[0].id;
    if (!firstMessageId) {
      console.warn('First message ID not found, cannot load more messages.');
      return;
    }

    isLoadingMoreRef.current = true;

    // Update loading state using sessionManager
    sessionManager.startLoadingEarlier();
    try {
      const data = await fetchSessionHistory(chatSessionId!, firstMessageId, FETCH_LIMIT);

      // Check if the session ID has changed while fetching history
      // If urlSessionId is different from chatSessionId, it means the user navigated to a new session
      if (urlSessionId !== chatSessionId) {
        console.log(
          `Ignoring loaded messages for old session ID: ${chatSessionId}. Current URL session ID: ${urlSessionId}`,
        );
        return; // Do not update messages for an old session
      }

      // Check if it's the last page
      const hasMore = data.history.length >= FETCH_LIMIT;
      // FSM will manage hasMoreMessages state through sessionManager
      sessionManager.completeLoadingEarlier(hasMore);

      // Prepend new messages to the existing ones
      const chatAreaElement = chatAreaRef.current;
      if (chatAreaElement) {
        const oldScrollHeight = chatAreaElement.scrollHeight;
        const oldScrollTop = chatAreaElement.scrollTop;

        setMessages((prevMessages) => [...mapToChatMessages(data.history || []), ...prevMessages]);

        // Use the new scroll adjustment hook
        adjustScroll(oldScrollHeight, oldScrollTop);
      } else {
        console.warn('loadMoreMessages: chatAreaElement is null. Cannot adjust scroll.');
        setMessages((prevMessages) => [...mapToChatMessages(data.history || []), ...prevMessages]);
      }
    } catch (error) {
      console.error('Failed to load more session history:', error);
      addErrorMessage('Failed to load more session history.');
      sessionManager.completeLoadingEarlier(false);
    } finally {
      isLoadingMoreRef.current = false;
    }
  }, [
    chatSessionId,
    addErrorMessage,
    setMessages,
    messages,
    processingStartTime,
    mapToChatMessages,
    chatAreaRef,
    adjustScroll,
    urlSessionId,
    sessionManager,
  ]);

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const redirectTo = params.get('redirect_to');
    const draftMessage = params.get('draft_message');

    if (draftMessage) {
      setInputMessage(draftMessage);
    }

    if (redirectTo) {
      if (redirectTo.startsWith('/')) {
        navigate(redirectTo, { replace: true });
      } else {
        console.warn('Invalid redirectTo URL detected, redirecting to home:', redirectTo);
        navigate('/', { replace: true });
      }
    }
  }, [location.search, navigate]);

  useEffect(() => {
    const loadChatSession = async () => {
      const currentSessionId = urlSessionId;

      if (location.pathname.endsWith('/new') && !currentSessionId) {
        // Handle preserveSelectedFiles before resetting state
        if (preserveSelectedFiles.length > 0) {
          setSelectedFiles(preserveSelectedFiles);
          setPreserveSelectedFiles([]);
        }

        resetChatSessionState();
        // workspaceId is now managed by FSM
        const defaultPrompt = `{{.Builtin.SystemPrompt}}`;
        setSystemPrompt(defaultPrompt);
        // Close existing EventSource if any, and mark as normally ended
        if (eventSourceRef.current) {
          isStreamEndedNormallyRef.current = true; // Mark as normally ended
          eventSourceRef.current.close();
          eventSourceRef.current = null;
        }
        setHasMoreMessages(false); // No more messages to load for a new session
        return;
      }

      if (currentSessionId && currentSessionId !== chatSessionId) {
        // Close any existing EventSource to stop streaming
        closeEventSourceNormally();

        // Complete streaming state in FSM
        sessionManager.completeStreaming();

        // Store sessionId immediately for use in EventInlineData handlers
        latestSessionIdRef.current = currentSessionId;

        // Start FSM session loading
        sessionManager.startSessionLoading(currentSessionId, urlWorkspaceId);

        // Reset manual model selection flag when loading a new session
        setIsModelManuallySelected(false);

        // Always preserve selected files during session navigation
        // Don't clear files unless explicitly done by user action
        if (preserveSelectedFiles.length > 0) {
          setSelectedFiles(preserveSelectedFiles);
          setPreserveSelectedFiles([]);
        }
        // Note: Don't clear existing selectedFiles - let them persist across session changes

        setProcessingStartTime(null);
        setMessages([]); // Clear messages from previous session
        setHasMoreMessages(true); // Always set to true when loading a new session

        try {
          eventSourceRef.current = loadSession(
            currentSessionId,
            FETCH_LIMIT,
            (event: MessageEvent) => {
              // First check: Is this EventSource still active?
              if (eventSourceRef.current !== activeEventSourceRef.current) {
                // This EventSource is no longer active, close it immediately
                const currentEventSource = event.target as EventSource;
                if (currentEventSource && currentEventSource.readyState !== EventSource.CLOSED) {
                  currentEventSource.close();
                }
                return;
              }

              const [eventType, eventData] = splitOnceByNewline(event.data);

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data: InitialState = JSON.parse(eventData);

                // sessionId is now managed by FSM
                setSystemPrompt(data.systemPrompt);
                setIsSystemPromptEditing(false);

                setMessages(mapToChatMessages(data.history || []));
                // workspaceId is now managed by FSM
                setPrimaryBranchId(data.primaryBranchId);
                setPendingConfirmation(data.pendingConfirmation || null);

                const hasMore = data.history && data.history.length >= FETCH_LIMIT;
                setHasMoreMessages(hasMore);

                // Update FSM state to mark session as loaded
                // Convert empty string to undefined
                const workspaceId = data.workspaceId || undefined;
                sessionManager.completeSessionLoading(data.sessionId, workspaceId, hasMore);

                // Handle initial envChanged message
                if (data.envChanged) {
                  const envChangedJsonString = JSON.stringify(data.envChanged);
                  const envChangedMessage: ChatMessage = {
                    id: crypto.randomUUID(),
                    type: 'env_changed',
                    parts: [{ text: envChangedJsonString }],
                    sessionId: data.sessionId,
                    branchId: data.primaryBranchId,
                  };
                  setTemporaryEnvChangeMessage(envChangedMessage);
                }

                // Scroll to bottom after initial messages are loaded
                requestAnimationFrame(() => {
                  if (chatAreaRef.current) {
                    chatAreaRef.current.scrollTop = chatAreaRef.current.scrollHeight;
                  }
                  setIsPriorSessionLoadComplete(true); // Set to true after initial load and scroll adjustment
                });

                if (eventType === EventInitialState && data.callElapsedTimeSeconds !== undefined) {
                  setProcessingStartTime(performance.now() - data.callElapsedTimeSeconds * 1000);
                  // Start streaming if we have elapsed time
                  sessionManager.startStreaming();
                } else {
                  setProcessingStartTime(null);
                }
                if (eventType === EventInitialStateNoCall) {
                  closeEventSourceNormally();
                }
              } else if (eventType === EventModelMessage) {
                const [messageId, text] = splitOnceByNewline(eventData);
                // Check if this message is for the current session
                if (urlSessionId !== currentSessionId) {
                  console.log(
                    `Ignoring ModelMessage for old session. Current URL session ID: ${urlSessionId}, expected: ${currentSessionId}`,
                  );
                  return;
                }
                updateAgentMessage({ messageId, text });
              } else if (eventType === EventFunctionCall) {
                const [messageId, rest] = splitOnceByNewline(eventData);
                const [functionName, argsJson] = splitOnceByNewline(rest);
                addMessage({
                  id: messageId,
                  parts: [
                    {
                      functionCall: {
                        name: functionName,
                        args: JSON.parse(argsJson),
                      },
                    },
                  ],
                  type: 'function_call',
                });
              } else if (eventType === EventFunctionResponse) {
                const [messageId, rest] = splitOnceByNewline(eventData);
                const [name, payloadJson] = splitOnceByNewline(rest);
                const { response, attachments } = JSON.parse(payloadJson);
                addMessage({
                  id: messageId,
                  parts: [{ functionResponse: { name, response } }],
                  type: 'function_response',
                  attachments,
                });
              } else if (eventType === EventInlineData) {
                const { messageId, attachments } = JSON.parse(eventData);
                addMessage({
                  id: messageId,
                  parts: [], // Empty parts for inline data messages
                  type: 'model',
                  attachments,
                  sessionId: latestSessionIdRef.current || undefined,
                });
              } else if (eventType === EventThought) {
                const [messageId, thoughtText] = splitOnceByNewline(eventData);
                addMessage({
                  id: messageId,
                  parts: [{ text: thoughtText }],
                  type: 'thought',
                });
              } else if (eventType === EventError) {
                console.error('SSE Error:', eventData);
                setProcessingStartTime(null);
                closeEventSourceNormally();
                addErrorMessage(eventData);
              } else if (eventType === EventComplete) {
                isStreamEndedNormallyRef.current = true; // Mark as normally ended
                setProcessingStartTime(null);
                // Complete streaming
                sessionManager.completeStreaming();
                closeEventSourceNormally();
              } else if (eventType === EventPendingConfirmation) {
                setPendingConfirmation(eventData);
              } else if (eventType === EventPing) {
                // Ping messages are ignored as they're only for connection keep-alive
                // No action needed, just continue processing other events
              } else if (eventType === EventWorkspaceHint) {
                // Handle workspace hint
                sessionManager.setSessionWorkspaceId(eventData);
              } else if (eventType === 'W') {
                // EventWorkspaceHint
                const workspaceId = eventData;
                sessionManager.setSessionWorkspaceId(workspaceId);
              }
            },
            (errorEvent: Event) => {
              console.error('EventSource error:', errorEvent);
              if (errorEvent.target && (errorEvent.target as EventSource).readyState === EventSource.CLOSED) {
                if (!isStreamEndedNormallyRef.current) {
                  // Use useRef value
                  // Only reload if not ended normally
                  window.location.reload();
                }
              }
              setProcessingStartTime(null);
            },
          );

          // Set this as the active EventSource after successful creation
          activeEventSourceRef.current = eventSourceRef.current;

          // Set as the globally active EventSource
          if (eventSourceRef.current) {
            sessionManager.setActiveEventSource(eventSourceRef.current);
          }
        } catch (error) {
          console.error('Failed to load session via SSE:', error);
          resetChatSessionState();
        }

        return () => {
          closeEventSourceNormally();
        };
      } else if (!currentSessionId) {
        resetChatSessionState();
        setHasMoreMessages(true); // Also set to true when session ID is null
      }
    };

    loadChatSession();
  }, [urlSessionId, urlWorkspaceId, navigate, location.pathname, processingStartTime, chatSessionId, sessionManager]);

  return { loadMoreMessages };
};
