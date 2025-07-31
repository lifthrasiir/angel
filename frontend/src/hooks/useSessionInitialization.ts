import { useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { ChatMessage } from '../types/chat';
import { loadSession } from '../utils/sessionManager';
import { fetchDefaultSystemPrompt } from '../utils/systemPromptManager';
import { fetchUserInfo } from '../utils/userManager';

import {
  SET_INPUT_MESSAGE,
  SET_CHAT_SESSION_ID,
  SET_MESSAGES,
  SET_SYSTEM_PROMPT,
  SET_IS_SYSTEM_PROMPT_EDITING,
  SET_SELECTED_FILES,
  SET_IS_STREAMING,
  SET_USER_EMAIL,
  RESET_CHAT_SESSION_STATE,
} from './chatReducer';
import { ChatAction } from './chatReducer';

interface UseSessionInitializationProps {
  chatSessionId: string | null;
  isStreaming: boolean;
  dispatch: React.Dispatch<ChatAction>;
  handleLoginRedirect: () => void;
  loadSessions: () => Promise<void>;
}

export const useSessionInitialization = ({
  chatSessionId,
  isStreaming,
  dispatch,
  handleLoginRedirect,
  loadSessions,
}: UseSessionInitializationProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId } = useParams();
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
      let currentSessionId = urlSessionId;
      if (!currentSessionId && location.pathname === '/new') {
        currentSessionId = 'new';
      }

      if (currentSessionId === 'new') {
        resetChatSessionState();
        const defaultPrompt = await fetchDefaultSystemPrompt();
        dispatch({ type: SET_SYSTEM_PROMPT, payload: defaultPrompt });
        return;
      }

      if (currentSessionId !== chatSessionId) {
        dispatch({ type: SET_SELECTED_FILES, payload: [] });
        dispatch({ type: SET_IS_STREAMING, payload: false });
      }

      if (currentSessionId) {
        try {
          const data = await loadSession(currentSessionId);
          if (data) {
            dispatch({ type: SET_CHAT_SESSION_ID, payload: data.sessionId });
            dispatch({ type: SET_SYSTEM_PROMPT, payload: data.systemPrompt });
            
            dispatch({ type: SET_IS_SYSTEM_PROMPT_EDITING, payload: false });
            
            if (!isStreaming) {
              dispatch({ type: SET_MESSAGES, payload: (data.history || []).map((msg: any) => {
                const chatMessage: ChatMessage = { ...msg, id: msg.id || crypto.randomUUID(), attachments: msg.attachments };
                if (msg.type === 'thought') {
                  chatMessage.type = 'thought';
                } else if (msg.type === 'model_error') {
                  chatMessage.type = 'model_error';
                } else if (msg.parts[0].functionCall) {
                  chatMessage.type = 'function_call';
                  chatMessage.parts[0] = { functionCall: msg.parts[0].functionCall };
                } else if (msg.parts[0].functionResponse) {
                  chatMessage.type = 'function_response';
                  chatMessage.parts[0] = { functionResponse: msg.parts[0].functionResponse };
                } else {
                  chatMessage.type = msg.role;
                }
                return chatMessage;
              }) });
            }
          } else {
            resetChatSessionState();
          }
        } catch (error) {
          if (error instanceof Error && error.message === 'UNAUTHORIZED') {
            handleLoginRedirect();
          } else {
            resetChatSessionState();
          }
        }
      } else {
        resetChatSessionState();
      }
    };

    initializeChatSession();
    loadSessions();

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
        console.error("Failed to fetch user info:", error);
        handleLoginRedirect();
      }
    };
    loadUserInfo();
  }, [urlSessionId, navigate, location.search, location.pathname, isStreaming, dispatch]);
};