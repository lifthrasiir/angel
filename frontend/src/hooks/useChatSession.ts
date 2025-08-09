import { useEffect } from 'react';
import { useLocation, useParams } from 'react-router-dom';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { handleLogin } from '../utils/userManager';
import { useChat } from './ChatContext';
import {
  SET_SELECTED_FILES,
  SET_SESSIONS,
  SET_WORKSPACE_ID,
  SET_WORKSPACE_NAME,
  SET_AVAILABLE_MODELS,
  SET_SELECTED_MODEL,
} from './chatReducer';
import { useDocumentTitle } from './useDocumentTitle';
import { useMessageSending } from './useMessageSending';
import { useSessionInitialization } from './useSessionInitialization';
import { useWorkspaceAndSessions } from './useWorkspaceAndSessions';
import { getAvailableModels, ModelInfo } from '../api/models'; // Import ModelInfo

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
    primaryBranchId,
    availableModels,
    selectedModel,
  } = state;

  const location = useLocation();
  const { workspaceId: urlWorkspaceId } = useParams<{ workspaceId?: string }>(); // Get workspaceId from URL

  // Pass stateWorkspaceId to useWorkspaceAndSessions
  const { currentWorkspace, sessions: fetchedSessions, error } = useWorkspaceAndSessions(stateWorkspaceId);

  useEffect(() => {
    const fetchModels = async () => {
      try {
        const modelsMap = await getAvailableModels();
        dispatch({ type: SET_AVAILABLE_MODELS, payload: modelsMap });
        if (modelsMap.size > 0) {
          // Get the first model from the map values
          const firstModel = modelsMap.values().next().value;
          dispatch({ type: SET_SELECTED_MODEL, payload: firstModel || null });
        }
      } catch (err) {
        console.error('Failed to fetch available models:', err);
      }
    };
    fetchModels();
  }, [dispatch]);

  useEffect(() => {
    if (currentWorkspace) {
      dispatch({ type: SET_WORKSPACE_NAME, payload: currentWorkspace.name });
    }
    if (fetchedSessions) {
      dispatch({ type: SET_SESSIONS, payload: fetchedSessions });
    }
    if (error) {
      console.error('Failed to load sessions:', error);
      // Optionally, dispatch an error state to your chatReducer
    }
  }, [currentWorkspace, fetchedSessions, error, dispatch]);

  const handleLoginRedirect = () => {
    const currentPath = location.pathname + location.search;
    handleLogin(currentPath, inputMessage);
  };

  const handleFilesSelectedWrapper = (files: File[]) => {
    dispatch({
      type: SET_SELECTED_FILES,
      payload: handleFilesSelected(selectedFiles, files),
    });
  };

  const handleRemoveFileWrapper = (index: number) => {
    dispatch({
      type: SET_SELECTED_FILES,
      payload: handleRemoveFile(selectedFiles, index),
    });
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
    primaryBranchId,
  });

  const { handleSendMessage, cancelStreamingCall } = useMessageSending({
    inputMessage,
    selectedFiles,
    chatSessionId,
    systemPrompt,
    dispatch,
    handleLoginRedirect,
    primaryBranchId,
    selectedModel,
  });

  const setSelectedModel = (model: ModelInfo) => {
    dispatch({ type: SET_SELECTED_MODEL, payload: model });
  };

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
    primaryBranchId,
    availableModels,
    selectedModel,
    handleLogin: handleLoginRedirect,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    cancelStreamingCall,
    setSelectedModel,
  };
};
