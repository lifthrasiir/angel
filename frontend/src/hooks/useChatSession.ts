import { useLocation } from 'react-router-dom';
import { fetchSessions } from '../utils/sessionManager';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { handleLogin } from '../utils/userManager';
import { useDocumentTitle } from './useDocumentTitle';
import { useSessionInitialization } from './useSessionInitialization';
import { useMessageSending } from './useMessageSending';
import { useChat } from './ChatContext';
import {
  SET_SESSIONS,
  SET_SELECTED_FILES,
} from './chatReducer';

export const useChatSession = () => {
  const { state, dispatch } = useChat();
  const {
    userEmail,
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    lastAutoDisplayedThoughtId,
    isStreaming,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
  } = state;

  const location = useLocation();

  const loadSessions = async () => {
    const sessionsData = await fetchSessions();
    dispatch({ type: SET_SESSIONS, payload: sessionsData });
  };

  const handleLoginRedirect = () => {
    const currentPath = location.pathname + location.search;
    handleLogin(currentPath, inputMessage);
  };

  const handleFilesSelectedWrapper = (files: File[]) => {
    dispatch({ type: SET_SELECTED_FILES, payload: handleFilesSelected(selectedFiles, files) });
  };

  const handleRemoveFileWrapper = (index: number) => {
    dispatch({ type: SET_SELECTED_FILES, payload: handleRemoveFile(selectedFiles, index) });
  };

  useDocumentTitle(sessions);

  useSessionInitialization({
    chatSessionId,
    isStreaming,
    dispatch,
    handleLoginRedirect,
    loadSessions,
  });

  const { handleSendMessage, cancelStreamingCall } = useMessageSending({
    inputMessage,
    selectedFiles,
    chatSessionId,
    systemPrompt,
    dispatch,
    handleLoginRedirect,
    loadSessions,
  });

  return {
    userEmail,
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    lastAutoDisplayedThoughtId,
    isStreaming,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
    handleLogin: handleLoginRedirect,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    fetchSessions: loadSessions,
    cancelStreamingCall,
  };
};