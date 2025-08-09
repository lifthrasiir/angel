import { useEffect } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useAtom, useSetAtom } from 'jotai';
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
import { fetchUserInfo } from '../utils/userManager';
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
  userEmailAtom,
  workspaceIdAtom,
  updateAgentMessageAtom,
} from '../atoms/chatAtoms';

interface UseSessionLoaderProps {
  chatSessionId: string | null;
  isStreaming: boolean;
  handleLoginRedirect: () => void;
  primaryBranchId: string;
}

export const useSessionLoader = ({
  chatSessionId,
  isStreaming,
  handleLoginRedirect,
  primaryBranchId,
}: UseSessionLoaderProps) => {
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
  const [messages] = useAtom(messagesAtom);
  const setPrimaryBranchId = useSetAtom(primaryBranchIdAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const setSystemPrompt = useSetAtom(systemPromptAtom);
  const setUserEmail = useSetAtom(userEmailAtom);
  const setWorkspaceId = useSetAtom(workspaceIdAtom);
  const addMessage = useSetAtom(addMessageAtom);
  const updateAgentMessage = useSetAtom(updateAgentMessageAtom);
  const addErrorMessage = useSetAtom(addErrorMessageAtom);
  const resetChatSessionState = useSetAtom(resetChatSessionStateAtom);

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
    const loadUserInfo = async () => {
      try {
        const userInfo = await fetchUserInfo();
        if (userInfo && userInfo.success) {
          if (userInfo.email) {
            setUserEmail(userInfo.email);
          } else {
            handleLoginRedirect();
          }
        } else {
          handleLoginRedirect();
        }
      } catch (error) {
        console.error('Failed to fetch user info:', error);
        handleLoginRedirect();
      }
    };
    loadUserInfo();
  }, [handleLoginRedirect, setUserEmail]);

  useEffect(() => {
    if (isStreaming) {
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
        return;
      }

      if (currentSessionId && currentSessionId !== chatSessionId) {
        setChatSessionId(currentSessionId);
        setSelectedFiles([]);
        setIsStreaming(false);
        setMessages([]); // Clear messages from previous session
      }

      if (currentSessionId) {
        let eventSource: EventSource | null = null;
        try {
          eventSource = loadSession(
            currentSessionId,
            primaryBranchId,
            (event: MessageEvent) => {
              const [eventType, eventData] = splitOnceByNewline(event.data);

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data: InitialState = JSON.parse(eventData);
                setChatSessionId(data.sessionId);
                setSystemPrompt(data.systemPrompt);
                setIsSystemPromptEditing(false);
                console.log('InitialState history:', data.history);

                // Only set history if messagesAtom is empty.
                // This prevents duplication if useMessageSending.ts is already adding messages.
                if (messages.length === 0) {
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
                      } else {
                        chatMessage.type = msg.role;
                      }
                      return chatMessage;
                    }),
                  );
                }
                setWorkspaceId(data.workspaceId);
                setPrimaryBranchId(data.primaryBranchId);

                setIsStreaming(eventType === EventInitialState);
                if (eventType === EventInitialStateNoCall) {
                  eventSource?.close();
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
                eventSource?.close();
                addErrorMessage(eventData);
              } else if (eventType === EventComplete) {
                setIsStreaming(false);
                eventSource?.close();
              }
            },
            (errorEvent: Event) => {
              console.error('EventSource error:', errorEvent);
              if (errorEvent.target && (errorEvent.target as EventSource).readyState === EventSource.CLOSED) {
                fetchUserInfo()
                  .then((userInfo) => {
                    if (!userInfo || !userInfo.success || !userInfo.email) {
                      handleLoginRedirect();
                    }
                  })
                  .catch(() => {
                    handleLoginRedirect();
                  });
              }
              setIsStreaming(false);
            },
          );
        } catch (error) {
          console.error('Failed to load session via SSE:', error);
          resetChatSessionState();
        }

        return () => {
          if (eventSource) {
            eventSource.close();
          }
        };
      } else {
        resetChatSessionState();
      }
    };

    loadChatSession();
  }, [urlSessionId, urlWorkspaceId, navigate, location.pathname, isStreaming, chatSessionId, primaryBranchId]);
};
