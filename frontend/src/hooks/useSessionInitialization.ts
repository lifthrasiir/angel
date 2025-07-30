import { useEffect } from 'react';
import { useNavigate, useParams, useLocation } from 'react-router-dom';
import { ChatMessage } from '../types/chat';
import { loadSession } from '../utils/sessionManager';
import { fetchDefaultSystemPrompt } from '../utils/systemPromptManager';
import { fetchUserInfo } from '../utils/userManager';

interface UseSessionInitializationProps {
  chatSessionId: string | null;
  isStreaming: boolean;
  setInputMessage: (message: string) => void;
  setChatSessionId: (id: string | null) => void;
  setMessages: (messages: ChatMessage[] | ((prev: ChatMessage[]) => ChatMessage[])) => void;
  setSystemPrompt: (prompt: string) => void;
  setIsSystemPromptEditing: (editing: boolean) => void;
  setSelectedFiles: (files: File[]) => void;
  setIsStreaming: (streaming: boolean) => void;
  setUserEmail: (email: string | null) => void;
  handleLoginRedirect: () => void;
  loadSessions: () => Promise<void>;
}

export const useSessionInitialization = ({
  chatSessionId,
  isStreaming,
  setInputMessage,
  setChatSessionId,
  setMessages,
  setSystemPrompt,
  setIsSystemPromptEditing,
  setSelectedFiles,
  setIsStreaming,
  setUserEmail,
  handleLoginRedirect,
  loadSessions,
}: UseSessionInitializationProps) => {
  const navigate = useNavigate();
  const { sessionId: urlSessionId } = useParams();
  const location = useLocation();

  const resetChatSessionState = () => {
    setChatSessionId(null);
    setMessages([]);
    setSystemPrompt('');
    setIsSystemPromptEditing(true);
    setSelectedFiles([]);
  };

  useEffect(() => {
    if (isStreaming) {
      return;
    }
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
        setSystemPrompt(defaultPrompt);
        return;
      }

      if (currentSessionId !== chatSessionId) {
        setSelectedFiles([]);
        setIsStreaming(false);
      }

      if (currentSessionId) {
        try {
          const data = await loadSession(currentSessionId);
          if (data) {
            setChatSessionId(data.sessionId);
            setSystemPrompt(data.systemPrompt);
            
            setIsSystemPromptEditing(false);
            
            if (!isStreaming) {
              setMessages((data.history || []).map((msg: any) => {
                const chatMessage: ChatMessage = { ...msg, id: msg.id || crypto.randomUUID(), attachments: msg.attachments };
                if (msg.type === 'thought') {
                  chatMessage.type = 'thought';
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
              }));
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
            setUserEmail(userInfo.email);
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
  }, [urlSessionId, navigate, location.search, location.pathname, isStreaming]);
};