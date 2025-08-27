import { useEffect, useRef, useCallback } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useSetAtom, useAtomValue } from 'jotai';
import type { ChatMessage, InitialState } from '../types/chat';
import {
  EventComplete,
  EventError,
  EventFunctionCall,
  EventFunctionReply,
  EventInitialState,
  EventInitialStateNoCall,
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

  const { adjustScroll } = useScrollAdjustment({ chatAreaRef });

  const closeEventSourceNormally = () => {
    if (eventSourceRef.current) {
      isStreamEndedNormallyRef.current = true;
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  };

  const mapToChatMessages = useCallback((rawMessages: any[]): ChatMessage[] => {
    return rawMessages.map((msg: any) => {
      const chatMessage: ChatMessage = {
        ...msg,
        id: msg.id,
        attachments: msg.attachments,
        cumulTokenCount: msg.cumul_token_count,
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
  }, []);

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
        // 기존 EventSource가 있다면 닫고, 정상 종료로 표시
        if (eventSourceRef.current) {
          isStreamEndedNormallyRef.current = true; // 정상 종료로 표시
          eventSourceRef.current.close();
          eventSourceRef.current = null;
        }
        setHasMoreMessages(false); // No more messages to load for a new session
        return;
      }

      if (currentSessionId && currentSessionId !== chatSessionId) {
        closeEventSourceNormally();
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

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data: InitialState = JSON.parse(eventData);
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
              } else if (eventType === EventFunctionReply) {
                const [messageId, functionResponseJson] = splitOnceByNewline(eventData);
                addMessage({
                  id: messageId,
                  parts: [{ functionResponse: JSON.parse(functionResponseJson) }],
                  type: 'function_response',
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
                isStreamEndedNormallyRef.current = true; // 정상 종료로 표시
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
                  // useRef 값 사용
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
