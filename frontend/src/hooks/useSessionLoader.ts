import { useEffect, useRef } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useSetAtom } from 'jotai';
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
} from '../utils/messageHandler';
import { loadSession } from '../utils/sessionManager';
import { splitOnceByNewline } from '../utils/stringUtils';
import {
  addErrorMessageAtom,
  addMessageAtom,
  resetChatSessionStateAtom,
  chatSessionIdAtom,
  inputMessageAtom,
  isStreamingAtom,
  isSystemPromptEditingAtom,
  messagesAtom,
  primaryBranchIdAtom,
  selectedFilesAtom,
  systemPromptAtom,
  workspaceIdAtom,
  updateAgentMessageAtom,
} from '../atoms/chatAtoms';

interface UseSessionLoaderProps {
  chatSessionId: string | null;
  isStreaming: boolean;
  primaryBranchId: string;
}

export const useSessionLoader = ({ chatSessionId, isStreaming, primaryBranchId }: UseSessionLoaderProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId, workspaceId: urlWorkspaceId } = useParams<{
    sessionId?: string;
    workspaceId?: string;
  }>();
  const location = useLocation();

  const setChatSessionId = useSetAtom(chatSessionIdAtom);
  const setInputMessage = useSetAtom(inputMessageAtom);
  const setIsStreaming = useSetAtom(isStreamingAtom);
  const setIsSystemPromptEditing = useSetAtom(isSystemPromptEditingAtom);
  const setMessages = useSetAtom(messagesAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const setWorkspaceId = useSetAtom(workspaceIdAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);

  const eventSourceRef = useRef<EventSource | null>(null);
  const isStreamEndedNormallyRef = useRef(false);

  const closeEventSourceNormally = () => {
    if (eventSourceRef.current) {
      isStreamEndedNormallyRef.current = true;
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  };

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
    // 스트리밍 중이거나, 현재 URL 세션 ID가 이미 로드된 세션 ID와 같으면 아무것도 하지 않음
    if (isStreaming || (urlSessionId && urlSessionId === chatSessionId)) {
      return;
    }

    const loadChatSession = async () => {
      const currentSessionId = urlSessionId;
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
        return;
      }

      if (currentSessionId && currentSessionId !== chatSessionId) {
        closeEventSourceNormally();
        setChatSessionId(currentSessionId);
        setSelectedFiles([]);
        setIsStreaming(false);
        setMessages([]); // Clear messages from previous session

        try {
          eventSourceRef.current = loadSession(
            currentSessionId,
            primaryBranchId,
            (event: MessageEvent) => {
              const [eventType, eventData] = splitOnceByNewline(event.data);

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data: InitialState = JSON.parse(eventData);
                setChatSessionId(data.sessionId);
                setSystemPrompt(data.systemPrompt);
                setIsSystemPromptEditing(false);

                setMessages(
                  (data.history || []).map((msg: any) => {
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
                    } else if (msg.type !== 'text') {
                      chatMessage.type = msg.type;
                    } else {
                      chatMessage.type = msg.role;
                    }
                    return chatMessage;
                  }),
                );
                setWorkspaceId(data.workspaceId);
                setPrimaryBranchId(data.primaryBranchId);

                setIsStreaming(eventType === EventInitialState);
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
                  role: 'model',
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
                  role: 'user',
                  parts: [{ functionResponse: JSON.parse(functionResponseJson) }],
                  type: 'function_response',
                });
              } else if (eventType === EventThought) {
                const [messageId, thoughtText] = splitOnceByNewline(eventData);
                addMessage({
                  id: messageId,
                  role: 'thought',
                  parts: [{ text: thoughtText }],
                  type: 'thought',
                });
              } else if (eventType === EventError) {
                console.error('SSE Error:', eventData);
                setIsStreaming(false);
                closeEventSourceNormally();
                addErrorMessage(eventData);
              } else if (eventType === EventComplete) {
                isStreamEndedNormallyRef.current = true; // 정상 종료로 표시
                setIsStreaming(false);
                closeEventSourceNormally();
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
              setIsStreaming(false);
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
      }
    };

    loadChatSession();
  }, [urlSessionId, urlWorkspaceId, navigate, location.pathname, isStreaming, chatSessionId, primaryBranchId]);
};
