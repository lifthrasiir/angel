import { useLocation, useParams } from 'react-router-dom';
import { useEffect } from 'react';
import { useWorkspaceAndSessions } from './useWorkspaceAndSessions';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { handleLogin } from '../utils/userManager';
import { useDocumentTitle } from './useDocumentTitle';
import { useSessionInitialization } from './useSessionInitialization';
import { useMessageSending } from './useMessageSending';
import { useChat } from './ChatContext';
import {
  SET_SESSIONS,
  SET_SELECTED_FILES,
  SET_WORKSPACE_NAME,
  SET_WORKSPACE_ID,
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
    workspaceId: stateWorkspaceId, // Rename to avoid conflict with useParams
    workspaceName,
  } = state;

  const location = useLocation();
  const { workspaceId: urlWorkspaceId } = useParams<{ workspaceId?: string }>(); // Get workspaceId from URL

  // Pass stateWorkspaceId to useWorkspaceAndSessions
  const { currentWorkspace, sessions: fetchedSessions, error } = useWorkspaceAndSessions(stateWorkspaceId);

  useEffect(() => {
    if (currentWorkspace) {
      dispatch({ type: SET_WORKSPACE_NAME, payload: currentWorkspace.name });
    }
    if (fetchedSessions) {
      dispatch({ type: SET_SESSIONS, payload: fetchedSessions });
    }
    if (error) {
      console.error("Failed to load sessions:", error);
      // Optionally, dispatch an error state to your chatReducer
    }
  }, [currentWorkspace, fetchedSessions, error, dispatch]);

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

  useEffect(() => {
    dispatch({ type: SET_WORKSPACE_ID, payload: urlWorkspaceId }); // Dispatch URL workspaceId to state
  }, [urlWorkspaceId, dispatch]);

  useSessionInitialization({
    chatSessionId,
    isStreaming,
    dispatch,
    handleLoginRedirect,
  });

  const { handleSendMessage, cancelStreamingCall } = useMessageSending({
    inputMessage,
    selectedFiles,
    chatSessionId,
    systemPrompt,
    dispatch,
    handleLoginRedirect,
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
    workspaceId: stateWorkspaceId, // Return stateWorkspaceId
    workspaceName,
    handleLogin: handleLoginRedirect,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    cancelStreamingCall,
  };
};
