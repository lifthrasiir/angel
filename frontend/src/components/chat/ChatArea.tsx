import type React from 'react';
import { useMemo } from 'react';
import { useAtom, useAtomValue } from 'jotai';
import { useLocation } from 'react-router-dom';
import SystemPromptEditor from './SystemPromptEditor';
import InputArea from '../InputArea';
import ConfirmationDialog from '../ConfirmationDialog';
import MessageListContainer from './MessageListContainer';
import ChatAreaDragDropOverlay from './ChatAreaDragDropOverlay';
import TemporarySessionNotice from './TemporarySessionNotice';
import { messagesAtom, systemPromptAtom, primaryBranchIdAtom } from '../../atoms/chatAtoms';
import { pendingConfirmationAtom } from '../../atoms/confirmationAtoms';
import { selectedFilesAtom } from '../../atoms/fileAtoms';
import { availableModelsAtom, globalPromptsAtom } from '../../atoms/modelAtoms';
import { isAuthenticatedAtom } from '../../atoms/systemAtoms';
import { isSystemPromptEditingAtom } from '../../atoms/uiAtoms';
import { useSessionFSM } from '../../hooks/useSessionFSM';
import { useProcessingState } from '../../hooks/useProcessingState';
import { useMessageGrouping } from '../../hooks/useMessageGrouping';
import { isNewTemporarySessionURL } from '../../utils/urlSessionMapping';

interface ChatAreaProps {
  handleSendMessage: () => void;
  onFilesSelected: (files: File[]) => void;
  handleRemoveFile: (index: number) => void;
  handleFileResizeStateChange?: (file: File, shouldResize: boolean) => void;
  handleFileProcessingStateChange?: (file: File, isProcessing: boolean) => void;
  handleFileResized?: (originalFile: File, resizedFile: File) => void;
  handleCancelStreaming: () => void;
  handleCancelMessageStreams: () => void;
  chatInputRef: React.RefObject<HTMLTextAreaElement>;
  chatAreaRef: React.RefObject<HTMLDivElement>;
  sendConfirmation: (
    approved: boolean,
    sessionId: string,
    branchId: string,
    modifiedData?: Record<string, any>,
  ) => Promise<void>;
  handleEditMessage: (originalMessageId: string, editedText: string) => Promise<void>;
  handleRetryMessage?: (originalMessageId: string) => Promise<void>;
  handleRetryError?: (errorMessageId: string) => Promise<void>;
  handleBranchSwitch: (newBranchId: string) => Promise<void>;
  isSendDisabledByResizing?: () => boolean;
}

const ChatArea: React.FC<ChatAreaProps> = ({
  handleSendMessage,
  onFilesSelected,
  handleRemoveFile,
  handleFileResizeStateChange,
  handleFileProcessingStateChange,
  handleFileResized,
  handleCancelStreaming,
  handleCancelMessageStreams,
  chatInputRef,
  chatAreaRef,
  sendConfirmation,
  handleEditMessage,
  handleRetryMessage,
  handleRetryError,
  handleBranchSwitch,
  isSendDisabledByResizing,
}) => {
  const [messages] = useAtom(messagesAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [isAuthenticated] = useAtom(isAuthenticatedAtom);
  const [systemPrompt, setSystemPrompt] = useAtom(systemPromptAtom);
  const isSystemPromptEditing = useAtomValue(isSystemPromptEditingAtom);
  const [globalPrompts] = useAtom(globalPromptsAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const location = useLocation();

  // Get processing state from custom hook
  const { startTime } = useProcessingState();

  // Use the new unified useSessionFSM hook
  const sessionFSM = useSessionFSM({
    onSessionSwitch: handleCancelMessageStreams,
  });

  const {
    sessionId,
    workspaceId,
    isLoading: isPriorSessionLoading,
    hasMoreMessages: hasMoreMessagesState,
    loadEarlierMessages,
  } = sessionFSM;
  const pendingConfirmation = useAtomValue(pendingConfirmationAtom);

  // Use message grouping hook
  const { renderedMessages } = useMessageGrouping({
    messages,
    availableModels,
    startTime: startTime ?? undefined,
    temporaryEnvChangeMessage: null, // Will be handled in the hook
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
  });

  // Check if this is a temporary session and show notice if needed
  const showTempSessionNotice = useMemo(() => {
    const isTempURL = isNewTemporarySessionURL(location.pathname);
    const isTempID = sessionId && sessionId.startsWith('.');
    return isTempURL || !!isTempID;
  }, [sessionId, location.pathname]);

  const currentSystemPromptLabel = useMemo(() => {
    const found = globalPrompts.find((p) => p.value === systemPrompt);
    return found ? found.label : ''; // Return label if found, else empty string for custom
  }, [systemPrompt, globalPrompts]);

  return (
    <ChatAreaDragDropOverlay onFilesSelected={onFilesSelected}>
      <MessageListContainer
        chatAreaRef={chatAreaRef}
        sessionId={sessionId ?? undefined}
        messages={messages}
        hasMoreMessages={hasMoreMessagesState}
        isLoading={isPriorSessionLoading}
        loadEarlierMessages={loadEarlierMessages}
      >
        {!hasMoreMessagesState && (
          <>
            <TemporarySessionNotice show={showTempSessionNotice} />
            <SystemPromptEditor
              key={sessionId || 'new'}
              initialPrompt={systemPrompt}
              currentLabel={currentSystemPromptLabel}
              onPromptUpdate={(updatedPrompt) => {
                setSystemPrompt(updatedPrompt.value);
              }}
              isEditing={isSystemPromptEditing}
              predefinedPrompts={globalPrompts}
              workspaceId={workspaceId ?? undefined}
            />
          </>
        )}
        {renderedMessages}
      </MessageListContainer>
      {pendingConfirmation ? (
        <ConfirmationDialog
          onConfirm={(modifiedData) => sendConfirmation(true, sessionId!, primaryBranchId!, modifiedData)}
          onDeny={() => sendConfirmation(false, sessionId!, primaryBranchId!)}
          confirmationData={JSON.parse(pendingConfirmation)}
        />
      ) : (
        <InputArea
          handleSendMessage={handleSendMessage}
          onFilesSelected={onFilesSelected}
          handleRemoveFile={handleRemoveFile}
          handleFileResizeStateChange={handleFileResizeStateChange}
          handleFileProcessingStateChange={handleFileProcessingStateChange}
          handleFileResized={handleFileResized}
          handleCancelStreaming={handleCancelStreaming}
          chatInputRef={chatInputRef}
          chatAreaRef={chatAreaRef}
          sessionId={sessionId}
          selectedFiles={selectedFiles}
          isSendDisabledByResizing={isSendDisabledByResizing}
          isDisabled={!isAuthenticated}
        />
      )}
    </ChatAreaDragDropOverlay>
  );
};

export default ChatArea;
