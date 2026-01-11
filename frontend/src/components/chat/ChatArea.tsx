import type React from 'react';
import { useEffect } from 'react';
import { useAtom, useAtomValue, useSetAtom } from 'jotai';
import InputArea from './InputArea';
import ConfirmationDialog from '../ConfirmationDialog';
import MessageListContainer from './MessageListContainer';
import ChatAreaDragDropOverlay from './ChatAreaDragDropOverlay';
import { messagesAtom, primaryBranchIdAtom } from '../../atoms/chatAtoms';
import { pendingConfirmationAtom } from '../../atoms/confirmationAtoms';
import { selectedFilesAtom } from '../../atoms/fileAtoms';
import { availableModelsAtom } from '../../atoms/modelAtoms';
import { isAuthenticatedAtom } from '../../atoms/systemAtoms';
import { lastAutoDisplayedThoughtIdAtom } from '../../atoms/uiAtoms';
import { useSessionFSM } from '../../hooks/useSessionFSM';
import { useProcessingState } from '../../hooks/useProcessingState';
import { useMessageGrouping } from '../../hooks/useMessageGrouping';

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
  handleUpdateMessage?: (messageId: string, editedText: string) => Promise<void>;
  handleContinueMessage?: (messageId: string) => Promise<void>;
  isSendDisabledByResizing?: () => boolean;
  chatHeader?: React.ReactNode;
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
  handleUpdateMessage,
  handleContinueMessage,
  isSendDisabledByResizing,
  chatHeader,
}) => {
  const [messages] = useAtom(messagesAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [isAuthenticated] = useAtom(isAuthenticatedAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const setLastAutoDisplayedThoughtId = useSetAtom(lastAutoDisplayedThoughtIdAtom);

  // Auto-expand the latest thought bubble during streaming
  useEffect(() => {
    if (messages.length === 0) {
      setLastAutoDisplayedThoughtId(null);
      return;
    }

    const lastMessage = messages[messages.length - 1];

    if (lastMessage.type === 'thought') {
      // Latest message is a thought, auto-expand it
      setLastAutoDisplayedThoughtId(lastMessage.id);
    } else {
      // Latest message is not a thought, clear auto-display
      setLastAutoDisplayedThoughtId(null);
    }
  }, [messages, setLastAutoDisplayedThoughtId]);

  // Get processing state from custom hook
  const { startTime } = useProcessingState();

  // Use the new unified useSessionFSM hook
  const sessionFSM = useSessionFSM({
    onSessionSwitch: handleCancelMessageStreams,
  });

  const {
    sessionId,
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
    handleUpdateMessage,
    handleContinueMessage,
  });

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        flex: 1,
        overflow: 'hidden',
        position: 'relative',
      }}
    >
      {chatHeader}
      <ChatAreaDragDropOverlay onFilesSelected={onFilesSelected}>
        <MessageListContainer
          chatAreaRef={chatAreaRef}
          sessionId={sessionId ?? undefined}
          messages={messages}
          hasMoreMessages={hasMoreMessagesState}
          isLoading={isPriorSessionLoading}
          loadEarlierMessages={loadEarlierMessages}
        >
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
    </div>
  );
};

export default ChatArea;
