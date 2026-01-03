import { useCallback, useEffect, useRef } from 'react';
import { useAtomValue, useSetAtom } from 'jotai';
import { handleFilesSelected, handleRemoveFile } from '../utils/fileHandler';
import { useAttachmentResize } from './useAttachmentResize';
import { useSessionManagerContext } from './SessionManagerContext';
import { getSessionId, getWorkspaceId } from '../utils/sessionStateHelpers';

import {
  messagesAtom,
  inputMessageAtom,
  sessionsAtom,
  systemPromptAtom,
  primaryBranchIdAtom,
} from '../atoms/chatAtoms';
import { pendingConfirmationAtom } from '../atoms/confirmationAtoms';
import {
  lastAutoDisplayedThoughtIdAtom,
  isSystemPromptEditingAtom,
  isModelManuallySelectedAtom,
} from '../atoms/uiAtoms';
import { selectedFilesAtom } from '../atoms/fileAtoms';
import { pendingRootsAtom } from '../atoms/fileAtoms';
import { workspaceNameAtom } from '../atoms/workspaceAtoms';
import { availableModelsAtom, selectedModelAtom } from '../atoms/modelAtoms';
import { useDocumentTitle } from './useDocumentTitle';
import { useSessionFSM } from './useSessionFSM';
import { getAvailableModels, ModelInfo } from '../api/models';

export const useChatSession = (isTemporary: boolean = false) => {
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
  const pendingRoots = useAtomValue(pendingRootsAtom);
  const setPendingRoots = useSetAtom(pendingRootsAtom);
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

  const { sendMessage, cancelCurrentOperation, confirmTool, switchBranch, retryMessage, editMessage, retryError } =
    useSessionFSM();

  const handleSetSelectedModel = (model: ModelInfo) => {
    setSelectedModel(model);
  };

  // Adapter functions to match old interface
  const isProcessing =
    sessionManager.sessionState.activeOperation !== 'none' && sessionManager.sessionState.activeOperation !== 'loading';

  const handleSendMessage = useCallback(
    (messageContent?: string) => {
      // Accept optional message content parameter
      // If not provided, fall back to the ref value
      const currentInput = messageContent !== undefined ? messageContent : inputMessageRef.current;
      const currentFiles = attachmentResize.getFilesForSending();

      // Send pendingRoots if available, then reset it
      const initialRoots = pendingRoots.length > 0 ? pendingRoots : undefined;
      if (initialRoots) {
        setPendingRoots([]);
      }

      sendMessage(currentInput, currentFiles, selectedModel, systemPrompt, stateWorkspaceId, initialRoots, isTemporary);
    },
    [
      sendMessage,
      selectedModel,
      systemPrompt,
      stateWorkspaceId,
      pendingRoots,
      setPendingRoots,
      isTemporary,
      attachmentResize,
    ],
  );

  const sendConfirmation = useCallback(
    async (approved: boolean, _sessionId: string, branchId: string, modifiedData?: Record<string, any>) => {
      if (approved) {
        confirmTool(branchId, modifiedData);
      }
    },
    [confirmTool],
  );

  const handleBranchSwitch = useCallback(
    async (newBranchId: string) => {
      switchBranch(newBranchId);
    },
    [switchBranch],
  );

  const handleRetryMessage = useCallback(
    async (originalMessageId: string) => {
      await retryMessage(originalMessageId);
    },
    [retryMessage],
  );

  const handleEditMessage = useCallback(
    async (originalMessageId: string, editedText: string) => {
      await editMessage(originalMessageId, editedText);
    },
    [editMessage],
  );

  const handleRetryError = useCallback(
    async (errorMessageId: string) => {
      await retryError(errorMessageId);
    },
    [retryError],
  );

  const cancelStreamingCall = useCallback(() => {
    cancelCurrentOperation();
  }, [cancelCurrentOperation]);

  const cancelActiveStreams = useCallback(() => {
    cancelCurrentOperation();
  }, [cancelCurrentOperation]);

  return {
    chatSessionId,
    messages,
    inputMessage,
    sessions,
    lastAutoDisplayedThoughtId,
    isProcessing,
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
