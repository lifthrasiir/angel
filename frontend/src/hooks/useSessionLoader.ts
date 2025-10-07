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
  EventThought,
  EventPendingConfirmation,
} from '../utils/messageHandler';
import { loadSession } from '../utils/sessionManager';
import { splitOnceByNewline } from '../utils/stringUtils';
import { fetchSessionHistory } from '../api/apiClient';
import {
  addErrorMessageAtom,
  addMessageAtom,
  resetChatSessionStateAtom,
  chatSessionIdAtom,
  inputMessageAtom,
  processingStartTimeAtom,
  isSystemPromptEditingAtom,
  messagesAtom,
  primaryBranchIdAtom,
  selectedFilesAtom,
  systemPromptAtom,
  workspaceIdAtom,
  updateAgentMessageAtom,
  isPriorSessionLoadingAtom,
  hasMoreMessagesAtom,
  isPriorSessionLoadCompleteAtom,
  pendingConfirmationAtom,
  temporaryEnvChangeMessageAtom,
} from '../atoms/chatAtoms';
import { useScrollAdjustment } from './useScrollAdjustment';

interface UseSessionLoaderProps {
  chatSessionId: string | null;
  primaryBranchId: string;
  chatAreaRef: React.RefObject<HTMLDivElement>;
}

const FETCH_LIMIT = 50;

export const useSessionLoader = ({ chatSessionId, primaryBranchId, chatAreaRef }: UseSessionLoaderProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId, workspaceId: urlWorkspaceId } = useParams<{
    sessionId?: string;
    workspaceId?: string;
  }>();
  const location = useLocation();

  const setChatSessionId = useSetAtom(chatSessionIdAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);
  const setProcessingStartTime = useSetAtom(processingStartTimeAtom);
  const setIsSystemPromptEditing = useSetAtom(isSystemPromptEditingAtom);
  const setIsPriorSessionLoading = useSetAtom(isPriorSessionLoadingAtom);
  const setHasMoreMessages = useSetAtom(hasMoreMessagesAtom);
  const setMessages = useSetAtom(messagesAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const setWorkspaceId = useSetAtom(workspaceIdAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);
  const setIsPriorSessionLoadComplete = useSetAtom(isPriorSessionLoadCompleteAtom);
  const setPendingConfirmation = useSetAtom(pendingConfirmationAtom);
  const setTemporaryEnvChangeMessage = useSetAtom(temporaryEnvChangeMessageAtom);

  const isPriorSessionLoading = useAtomValue(isPriorSessionLoadingAtom);
  const hasMoreMessages = useAtomValue(hasMoreMessagesAtom);
  const messages = useAtomValue(messagesAtom);
  const processingStartTime = useAtomValue(processingStartTimeAtom);

  const eventSourceRef = useRef<EventSource | null>(null);
  const isStreamEndedNormallyRef = useRef(false);
  const isLoadingMoreRef = useRef(false);
  const latestSessionIdRef = useRef<string | null>(null);

  const { adjustScroll } = useScrollAdjustment({ chatAreaRef });

  const closeEventSourceNormally = () => {
    if (eventSourceRef.current) {
      isStreamEndedNormallyRef.current = true;
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
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

    // Use the values from the top-level
    // Don't load more messages while streaming is in progress
    if (messages.length === 0 || isPriorSessionLoading || !hasMoreMessages || processingStartTime !== null) {
      return;
    }

    const firstMessageId = messages[0].id;
    if (!firstMessageId) {
      console.warn('First message ID not found, cannot load more messages.');
      return;
    }

    isLoadingMoreRef.current = true;
    setIsPriorSessionLoading(true);
    try {
      const data = await fetchSessionHistory(chatSessionId!, primaryBranchId, firstMessageId, FETCH_LIMIT);

      // Check if the session ID has changed while fetching history
      // If urlSessionId is different from chatSessionId, it means the user navigated to a new session
      if (urlSessionId !== chatSessionId) {
        console.log(
          `Ignoring loaded messages for old session ID: ${chatSessionId}. Current URL session ID: ${urlSessionId}`,
        );
        return; // Do not update messages for an old session
      }

      // Check if it's the last page
      setHasMoreMessages(data.history.length >= FETCH_LIMIT);

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
    } finally {
      isLoadingMoreRef.current = false;
      setIsPriorSessionLoading(false);
    }
  }, [
    chatSessionId,
    primaryBranchId,
    isPriorSessionLoading,
    hasMoreMessages,
    addErrorMessage,
    setIsPriorSessionLoading,
    setHasMoreMessages,
    setMessages,
    messages,
    processingStartTime,
    mapToChatMessages,
    chatAreaRef,
    adjustScroll,
    urlSessionId,
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

      // If a message is currently being processed, do not load the session history again.
      if (processingStartTime !== null) {
        return;
      }

      if (location.pathname.endsWith('/new') && !currentSessionId) {
        resetChatSessionState();
        if (urlWorkspaceId) {
          setWorkspaceId(urlWorkspaceId);
        } else {
          setWorkspaceId('');
        }
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
        closeEventSourceNormally();

        // Store sessionId immediately for use in EventInlineData handlers
        latestSessionIdRef.current = currentSessionId;

        setChatSessionId(currentSessionId);
        setSelectedFiles([]);
        setProcessingStartTime(null);
        setMessages([]); // Clear messages from previous session
        setHasMoreMessages(true); // Always set to true when loading a new session

        try {
          eventSourceRef.current = loadSession(
            currentSessionId,
            primaryBranchId,
            FETCH_LIMIT,
            (event: MessageEvent) => {
              const [eventType, eventData] = splitOnceByNewline(event.data);

              // Check if the current URL's session ID matches the session ID this EventSource was started with.
              // urlSessionId is the latest URL session ID captured in the useEffect's closure.
              // currentSessionId is the session ID at the time this loadChatSession call was made.
              // Additionally, check if the current active chat session ID (from Jotai atom) matches.
              if (urlSessionId !== currentSessionId) {
                console.log(
                  `Ignoring SSE event for old session ID: ${currentSessionId}. Current URL session ID: ${urlSessionId}`,
                );
                // Close this EventSource as it's no longer valid.
                eventSourceRef.current?.close();
                eventSourceRef.current = null;
                return;
              }

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data: InitialState = JSON.parse(eventData);
                // Before setting messages, ensure the session ID is still valid
                if (urlSessionId !== data.sessionId) {
                  console.log(
                    `Ignoring InitialState for old session ID: ${data.sessionId}. Current URL session ID: ${urlSessionId}`,
                  );
                  return;
                }
                setChatSessionId(data.sessionId);
                setSystemPrompt(data.systemPrompt);
                setIsSystemPromptEditing(false);

                setMessages(mapToChatMessages(data.history || []));
                setWorkspaceId(data.workspaceId);
                setPrimaryBranchId(data.primaryBranchId);
                setPendingConfirmation(data.pendingConfirmation || null);

                setHasMoreMessages(data.history && data.history.length >= FETCH_LIMIT);

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
                } else {
                  setProcessingStartTime(null);
                }
                if (eventType === EventInitialStateNoCall) {
                  closeEventSourceNormally();
                }
              } else if (eventType === EventModelMessage) {
                const [messageId, text] = splitOnceByNewline(eventData);
                // Before updating, ensure the message belongs to the current active session
                // Assuming messageId can be used to infer session or message object has sessionId
                // For now, relying on the outer check. If issues persist, might need to pass sessionId in payload.
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
                closeEventSourceNormally();
              } else if (eventType === EventPendingConfirmation) {
                setPendingConfirmation(eventData);
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
  }, [urlSessionId, urlWorkspaceId, navigate, location.pathname, primaryBranchId, processingStartTime]);

  return { loadMoreMessages };
};
