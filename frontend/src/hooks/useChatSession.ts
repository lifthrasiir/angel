import { useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useAtomValue, useSetAtom } from 'jotai';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';

import {
  userEmailAtom,
  chatSessionIdAtom,
  messagesAtom,
  inputMessageAtom,
  sessionsAtom,
  lastAutoDisplayedThoughtIdAtom,
  processingStartTimeAtom,
  systemPromptAtom,
  isSystemPromptEditingAtom,
  selectedFilesAtom,
  workspaceIdAtom,
  workspaceNameAtom,
  primaryBranchIdAtom,
  availableModelsAtom,
  selectedModelAtom,
  pendingConfirmationAtom,
} from '../atoms/chatAtoms';
import { useDocumentTitle } from './useDocumentTitle';
import { useMessageSending } from './useMessageSending';
import { useWorkspaceAndSessions } from './useWorkspaceAndSessions';
import { getAvailableModels, ModelInfo } from '../api/models';

export const useChatSession = () => {
  const userEmail = useAtomValue(userEmailAtom);
  const chatSessionId = useAtomValue(chatSessionIdAtom);
  const messages = useAtomValue(messagesAtom);
  const inputMessageRef = useRef('');
  const inputMessage = useAtomValue(inputMessageAtom);

  useEffect(() => {
    inputMessageRef.current = inputMessage;
  }, [inputMessage]);
  const sessions = useAtomValue(sessionsAtom);
  const lastAutoDisplayedThoughtId = useAtomValue(lastAutoDisplayedThoughtIdAtom);
  const processingStartTime = useAtomValue(processingStartTimeAtom);
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
  const pendingConfirmation = useAtomValue(pendingConfirmationAtom);

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

  const { handleSendMessage, cancelStreamingCall, sendConfirmation, handleEditMessage, handleBranchSwitch } =
    useMessageSending({
      inputMessage,
      selectedFiles,
      chatSessionId,
      systemPrompt,
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
    isProcessing: processingStartTime !== null,
    systemPrompt,
    isSystemPromptEditing,
    selectedFiles,
    workspaceId: stateWorkspaceId,
    workspaceName,
    primaryBranchId,
    availableModels,
    selectedModel,
    pendingConfirmation,
    handleFilesSelected: handleFilesSelectedWrapper,
    handleRemoveFile: handleRemoveFileWrapper,
    handleSendMessage,
    cancelStreamingCall,
    handleSetSelectedModel,
    sendConfirmation,
    handleEditMessage,
    handleBranchSwitch,
  };
};
