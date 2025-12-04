import { useEffect, useRef } from 'react';
import { useAtomValue, useSetAtom } from 'jotai';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { useAttachmentResize } from './useAttachmentResize';
import { useSessionManagerContext } from './SessionManagerContext';
import { getSessionId, getWorkspaceId } from '../utils/sessionStateHelpers';

import {
  messagesAtom,
  inputMessageAtom,
  sessionsAtom,
  lastAutoDisplayedThoughtIdAtom,
  processingStartTimeAtom,
  systemPromptAtom,
  isSystemPromptEditingAtom,
  selectedFilesAtom,
  workspaceNameAtom,
  primaryBranchIdAtom,
  availableModelsAtom,
  selectedModelAtom,
  pendingConfirmationAtom,
  isModelManuallySelectedAtom,
} from '../atoms/chatAtoms';
import { useDocumentTitle } from './useDocumentTitle';
import { useMessageSending } from './useMessageSending';
import { getAvailableModels, ModelInfo } from '../api/models';

export const useChatSession = () => {
  // Use sessionManager for sessionId and workspaceId
  const sessionManager = useSessionManagerContext();
  const chatSessionId = getSessionId(sessionManager.sessionState);
  const stateWorkspaceId = getWorkspaceId(sessionManager.sessionState);

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
  const workspaceName = useAtomValue(workspaceNameAtom);
  // workspaceName is now set by Sidebar component
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const availableModels = useAtomValue(availableModelsAtom);
  const setAvailableModels = useSetAtom(availableModelsAtom);
  const selectedModel = useAtomValue(selectedModelAtom);
  const setSelectedModel = useSetAtom(selectedModelAtom);
  const pendingConfirmation = useAtomValue(pendingConfirmationAtom);
  const isModelManuallySelected = useAtomValue(isModelManuallySelectedAtom);

  // workspaceId is now managed by FSM, no need to get from URL params
  // Session list loading is now handled by Sidebar component

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
    if (messages.length > 0 && !isModelManuallySelected) {
      const lastMessage = messages[messages.length - 1];
      if (lastMessage.model) {
        const modelToSelect = availableModels.get(lastMessage.model);
        if (modelToSelect) {
          setSelectedModel(modelToSelect);
        }
      }
    }
  }, [messages, availableModels, setSelectedModel, isModelManuallySelected]);

  const handleFilesSelectedWrapper = (files: File[]) => {
    setSelectedFiles(handleFilesSelected(selectedFiles, files));
  };

  // Use the dedicated attachment resize hook
  const attachmentResize = useAttachmentResize(selectedFiles);

  const handleRemoveFileWrapper = (index: number) => {
    const removedFile = selectedFiles[index];
    setSelectedFiles(handleRemoveFile(selectedFiles, index));

    // Clean up attachment resize state when file is removed
    if (removedFile) {
      attachmentResize.handleFileRemoved(removedFile);
    }
  };

  useDocumentTitle(sessions);

  // workspaceId is now managed by FSM - no need to set it here

  const {
    handleSendMessage,
    cancelStreamingCall,
    cancelActiveStreams,
    sendConfirmation,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
  } = useMessageSending({
    inputMessage,
    selectedFiles: attachmentResize.getFilesForSending(),
    chatSessionId,
    systemPrompt,
    primaryBranchId,
    selectedModel,
    sessionManager,
  });

  const handleSetSelectedModel = (model: ModelInfo) => {
    setSelectedModel(model);
  };

  return {
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
    handleFileResizeStateChange: attachmentResize.handleFileResizeStateChange,
    handleFileProcessingStateChange: attachmentResize.handleFileProcessingStateChange,
    handleFileResized: attachmentResize.handleResizedFileAvailable,
    isSendDisabledByResizing: attachmentResize.isSendDisabledByResizing,
    getFilesForSending: attachmentResize.getFilesForSending,
    handleSendMessage,
    cancelStreamingCall,
    cancelActiveStreams,
    handleSetSelectedModel,
    sendConfirmation,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
  };
};
