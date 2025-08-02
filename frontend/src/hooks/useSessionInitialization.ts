import { useEffect } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import type { ChatMessage } from '../types/chat';
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
  ADD_ERROR_MESSAGE,
  ADD_MESSAGE,
  type ChatAction,
  RESET_CHAT_SESSION_STATE,
  SET_CHAT_SESSION_ID,
  SET_INPUT_MESSAGE,
  SET_IS_STREAMING,
  SET_IS_SYSTEM_PROMPT_EDITING,
  SET_MESSAGES,
  SET_SELECTED_FILES,
  SET_SYSTEM_PROMPT,
  SET_USER_EMAIL,
  SET_WORKSPACE_ID, // Import SET_WORKSPACE_ID
  UPDATE_AGENT_MESSAGE,
} from './chatReducer';

interface UseSessionInitializationProps {
  chatSessionId: string | null;
  isStreaming: boolean;
  dispatch: React.Dispatch<ChatAction>;
  handleLoginRedirect: () => void;
}

export const useSessionInitialization = ({
  chatSessionId,
  isStreaming,
  dispatch,
  handleLoginRedirect,
}: UseSessionInitializationProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId, workspaceId: urlWorkspaceId } = useParams<{
    sessionId?: string;
    workspaceId?: string;
  }>(); // Renamed to urlWorkspaceId
  const location = useLocation();

  const resetChatSessionState = () => {
    dispatch({ type: RESET_CHAT_SESSION_STATE });
  };

  useEffect(() => {
    if (isStreaming) {
      return;
    }
    const params = new URLSearchParams(location.search);
    const redirectTo = params.get('redirect_to');
    const draftMessage = params.get('draft_message');

    if (draftMessage) {
      dispatch({ type: SET_INPUT_MESSAGE, payload: draftMessage });
    }

    if (redirectTo) {
      if (redirectTo.startsWith('/')) {
        navigate(redirectTo, { replace: true });
      } else {
        console.warn('Invalid redirectTo URL detected, redirecting to home:', redirectTo);
        navigate('/', { replace: true });
      }
      return;
    }

    const initializeChatSession = async () => {
      const currentSessionId = urlSessionId;
      // If the path ends with /new and no sessionId is provided in the URL, treat it as a new session
      if (location.pathname.endsWith('/new') && !currentSessionId) {
        resetChatSessionState();
        // If navigating to /w/:workspaceId/new, set the workspaceId in state
        if (urlWorkspaceId) {
          dispatch({ type: SET_WORKSPACE_ID, payload: urlWorkspaceId });
        } else {
          // /new 경로일 경우, 기본 워크스페이스를 나타내기 위해 빈 문자열로 설정
          dispatch({ type: SET_WORKSPACE_ID, payload: '' });
        }
        const defaultPrompt = `{{.Builtin.SystemPrompt}}`;
        dispatch({ type: SET_SYSTEM_PROMPT, payload: defaultPrompt });
        return;
      }

      if (currentSessionId !== chatSessionId) {
        dispatch({ type: SET_SELECTED_FILES, payload: [] });
        dispatch({ type: SET_IS_STREAMING, payload: false });
      }

      if (currentSessionId) {
        let eventSource: EventSource | null = null;
        try {
          eventSource = loadSession(
            currentSessionId,
            (event: MessageEvent) => {
              const [eventType, eventData] = splitOnceByNewline(event.data);

              if (eventType === EventInitialState || eventType === EventInitialStateNoCall) {
                const data = JSON.parse(eventData);
                dispatch({
                  type: SET_CHAT_SESSION_ID,
                  payload: data.sessionId,
                });
                dispatch({
                  type: SET_SYSTEM_PROMPT,
                  payload: data.systemPrompt,
                });
                dispatch({
                  type: SET_IS_SYSTEM_PROMPT_EDITING,
                  payload: false,
                });
                dispatch({
                  type: SET_MESSAGES,
                  payload: (data.history || []).map((msg: any) => {
                    const chatMessage: ChatMessage = {
                      ...msg,
                      id: msg.id || crypto.randomUUID(),
                      attachments: msg.attachments,
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
                });
                dispatch({ type: SET_WORKSPACE_ID, payload: data.workspaceId });

                dispatch({
                  type: SET_IS_STREAMING,
                  payload: eventType === EventInitialState,
                });
                if (eventType === EventInitialStateNoCall) {
                  eventSource?.close();
                }
              } else if (eventType === EventModelMessage) {
                dispatch({ type: UPDATE_AGENT_MESSAGE, payload: eventData });
              } else if (eventType === EventFunctionCall) {
                const [functionName, argsJson] = splitOnceByNewline(eventData);
                dispatch({
                  type: ADD_MESSAGE,
                  payload: {
                    id: crypto.randomUUID(),
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
                  },
                });
              } else if (eventType === EventFunctionReply) {
                dispatch({
                  type: ADD_MESSAGE,
                  payload: {
                    id: crypto.randomUUID(),
                    role: 'user',
                    parts: [{ functionResponse: JSON.parse(eventData) }],
                    type: 'function_response',
                  },
                });
              } else if (eventType === EventThought) {
                dispatch({
                  type: ADD_MESSAGE,
                  payload: {
                    id: crypto.randomUUID(),
                    role: 'thought',
                    parts: [{ text: eventData }],
                    type: 'thought',
                  },
                });
              } else if (eventType === EventError) {
                console.error('SSE Error:', eventData);
                dispatch({ type: SET_IS_STREAMING, payload: false });
                eventSource?.close(); // Close EventSource on error
                dispatch({ type: ADD_ERROR_MESSAGE, payload: eventData });
              } else if (eventType === EventComplete) {
                dispatch({ type: SET_IS_STREAMING, payload: false });
                eventSource?.close(); // Close EventSource on stream finished
              }
            },
            (errorEvent: Event) => {
              console.error('EventSource error:', errorEvent);
              // Handle connection errors, e.g., redirect to login if unauthorized
              if (errorEvent.target && (errorEvent.target as EventSource).readyState === EventSource.CLOSED) {
                // Connection closed, might be due to server error or network issue
                // Check if it's an auth issue by trying to fetch user info again
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
              dispatch({ type: SET_IS_STREAMING, payload: false });
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

    initializeChatSession();

    const loadUserInfo = async () => {
      try {
        const userInfo = await fetchUserInfo();
        if (userInfo && userInfo.success) {
          if (userInfo.email) {
            dispatch({ type: SET_USER_EMAIL, payload: userInfo.email });
          } else {
            // 401 response - not authenticated, redirect to login
            handleLoginRedirect();
          }
        } else {
          // Network or other error, redirect to login
          handleLoginRedirect();
        }
      } catch (error) {
        console.error('Failed to fetch user info:', error);
        handleLoginRedirect();
      }
    };
    loadUserInfo();
  }, [urlSessionId, urlWorkspaceId, navigate, location.search, location.pathname, isStreaming, dispatch]);
};
