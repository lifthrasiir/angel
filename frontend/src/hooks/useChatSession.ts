import { useCallback, useEffect, useRef } from 'react';
import { useLocation, useParams } from 'react-router-dom';
import { useAtomValue, useSetAtom } from 'jotai';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { handleLogin } from '../utils/userManager';
import {
  userEmailAtom,
  chatSessionIdAtom,
  messagesAtom,
  inputMessageAtom,
  sessionsAtom,
  lastAutoDisplayedThoughtIdAtom,
  isStreamingAtom,
  systemPromptAtom,
  isSystemPromptEditingAtom,
  selectedFilesAtom,
  workspaceIdAtom,
  workspaceNameAtom,
  primaryBranchIdAtom,
  availableModelsAtom,
  selectedModelAtom,
} from '../atoms/chatAtoms';
import { useDocumentTitle } from './useDocumentTitle';
import { useMessageSending } from './useMessageSending';
import { useSessionInitialization } from './useSessionInitialization';
import { useWorkspaceAndSessions } from './useWorkspaceAndSessions';
import { getAvailableModels, ModelInfo } from '../api/models';

export const useChatSession = () => {
  const userEmail = useAtomValue(userEmailAtom);
  const chatSessionId = useAtomValue(chatSessionIdAtom);
  const messages = useAtomValue(messagesAtom);
  const inputMessage = useAtomValue(inputMessageAtom);
  const inputMessageRef = useRef(inputMessage);

  useEffect(() => {
    inputMessageRef.current = inputMessage;
  }, [inputMessage]);
  const sessions = useAtomValue(sessionsAtom);
  const lastAutoDisplayedThoughtId = useAtomValue(lastAutoDisplayedThoughtIdAtom);
  const isStreaming = useAtomValue(isStreamingAtom);
  const systemPrompt = useAtomValue(systemPromptAtom);
  const isSystemPromptEditing = useAtomValue(isSystemPromptEditingAtom);
  const selectedFiles = useAtomValue(selectedFilesAtom);
  const setSelectedFiles = useSetAtom(selectedFilesAtom);
  const stateWorkspaceId = useAtomValue(workspaceIdAtom);
  const setWorkspaceId = useSetAtom(workspaceIdAtom);
  const workspaceName = useAtomValue(workspaceNameAtom);
  const setWorkspaceName = useSetAtom(workspaceNameAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const availableModels = useAtomValue(availableModelsAtom);
  const setAvailableModels = useSetAtom(availableModelsAtom);
  const selectedModel = useAtomValue(selectedModelAtom);
  const setSelectedModel = useSetAtom(selectedModelAtom);

  const location = useLocation();
  const { workspaceId: urlWorkspaceId } = useParams<{ workspaceId?: string }>();

  const { currentWorkspace, error } = useWorkspaceAndSessions(stateWorkspaceId);

  useEffect(() => {
    const fetchModels = async () => {
      try {
        const modelsMap = await getAvailableModels();
        setAvailableModels(modelsMap);
        if (modelsMap.size > 0) {
          const firstModel = modelsMap.values().next().value;
          setSelectedModel(firstModel || null);
        }
      } catch (err) {
        console.error('Failed to fetch available models:', err);
      }
    };
    fetchModels();
  }, [setAvailableModels, setSelectedModel]);

  useEffect(() => {
    if (currentWorkspace) {
      setWorkspaceName(currentWorkspace.name);
    }
    if (error) {
      console.error('Failed to load sessions:', error);
    }
  }, [currentWorkspace, error, setWorkspaceName]);

  useEffect(() => {
    if (messages.length > 0) {
      const lastMessage = messages[messages.length - 1];
      if (lastMessage.model) {
        const modelToSelect = availableModels.get(lastMessage.model);
        if (modelToSelect) {
          setSelectedModel(modelToSelect);
        }
      }
    }
  }, [messages, availableModels, setSelectedModel]);

  const handleLoginRedirect = useCallback(() => {
    const currentPath = location.pathname + location.search;
    handleLogin(currentPath, inputMessageRef.current);
  }, [location.pathname, location.search]);

  const handleFilesSelectedWrapper = (files: File[]) => {
    setSelectedFiles(handleFilesSelected(selectedFiles, files));
  };

  const handleRemoveFileWrapper = (index: number) => {
    setSelectedFiles(handleRemoveFile(selectedFiles, index));
  };

  useDocumentTitle(sessions);

  useEffect(() => {
    setWorkspaceId(urlWorkspaceId);
  }, [urlWorkspaceId, setWorkspaceId]);

  useSessionInitialization({
    chatSessionId,
    isStreaming,
    handleLoginRedirect,
    primaryBranchId,
  });

  const { handleSendMessage, cancelStreamingCall } = useMessageSending({
    inputMessage,
    selectedFiles,
    chatSessionId,
    systemPrompt,
    handleLoginRedirect,
    primaryBranchId,
    selectedModel,
  });

  const handleSetSelectedModel = (model: ModelInfo) => {
    setSelectedModel(model);
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
    workspaceId: stateWorkspaceId,
    workspaceName,
    primaryBranchId,
    availableModels,
    selectedModel,
    handleLogin: handleLoginRedirect,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    cancelStreamingCall,
    handleSetSelectedModel,
  };
};
