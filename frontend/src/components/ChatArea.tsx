import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useAtom, useAtomValue } from 'jotai';
import { useLocation } from 'react-router-dom';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import ChatMessage from './ChatMessage';
import SystemPromptEditor from './SystemPromptEditor';
import InputArea from './InputArea';
import { ThoughtGroup } from './ThoughtGroup';
import FunctionPairMessage from './FunctionPairMessage';
import ConfirmationDialog from './ConfirmationDialog';
import {
  messagesAtom,
  selectedFilesAtom,
  availableModelsAtom,
  isAuthenticatedAtom,
  systemPromptAtom,
  isSystemPromptEditingAtom,
  globalPromptsAtom,
  primaryBranchIdAtom,
  pendingConfirmationAtom,
  temporaryEnvChangeMessageAtom,
} from '../atoms/chatAtoms';
import { ProcessingIndicator } from './ProcessingIndicator';
import MessageInfo from './MessageInfo';
import { useSessionFSM } from '../hooks/useSessionFSM';
import { useProcessingState } from '../hooks/useProcessingState';
import { useScrollAdjustment } from '../hooks/useScrollAdjustment';
import { isNewTemporarySessionURL } from '../utils/urlSessionMapping';

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
  const [isDragging, setIsDragging] = useState(false); // State for drag and drop
  const pendingConfirmation = useAtomValue(pendingConfirmationAtom);
  const temporaryEnvChangeMessage = useAtomValue(temporaryEnvChangeMessageAtom);

  // State for temporary session notice
  const [showTempSessionNotice, setShowTempSessionNotice] = useState(false);

  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Scroll management
  const { scrollToBottom, handleContentLoad, adjustScroll } = useScrollAdjustment({ chatAreaRef });
  const lastMessageIdRef = useRef<string | null>(null);
  const isInitialLoadRef = useRef(true);
  const firstMessageIdRef = useRef<string | null>(null);
  const scrollStateRef = useRef({ scrollHeight: 0, scrollTop: 0 });

  // Reset initial load flag when session changes
  useEffect(() => {
    isInitialLoadRef.current = true;
    lastMessageIdRef.current = null;
    firstMessageIdRef.current = null;
    scrollStateRef.current = { scrollHeight: 0, scrollTop: 0 };
  }, [sessionId]);

  // Auto-scroll to bottom on initial load
  useEffect(() => {
    if (messages.length > 0 && isInitialLoadRef.current) {
      isInitialLoadRef.current = false;
      scrollToBottom();
    }
  }, [messages.length, scrollToBottom]);

  // Adjust scroll position when earlier messages are loaded (prepended)
  useEffect(() => {
    if (messages.length === 0) return;

    const chatArea = chatAreaRef.current;
    if (!chatArea) return;

    const firstMessageId = messages[0].id;

    // Save current scroll state BEFORE checking for changes
    const currentScrollState = {
      scrollHeight: chatArea.scrollHeight,
      scrollTop: chatArea.scrollTop,
    };

    // If first message changed, earlier messages were loaded
    if (firstMessageIdRef.current !== null && firstMessageIdRef.current !== firstMessageId) {
      // Use the saved scroll state from PREVIOUS render (before new messages were added)
      console.log('Adjusting scroll - old:', scrollStateRef.current, 'new:', currentScrollState);
      adjustScroll(scrollStateRef.current.scrollHeight, scrollStateRef.current.scrollTop);
    }

    // Update refs for next render
    firstMessageIdRef.current = firstMessageId;
    scrollStateRef.current = currentScrollState;
  }, [messages, chatAreaRef, adjustScroll]);

  // Auto-scroll to bottom when new messages arrive at the end
  useEffect(() => {
    if (messages.length === 0) return;

    const lastMessage = messages[messages.length - 1];
    const lastMessageId = lastMessage.id;

    // Only scroll if the last message changed (new message at end)
    if (lastMessageIdRef.current !== lastMessageId) {
      lastMessageIdRef.current = lastMessageId;
      scrollToBottom();
    }
  }, [messages, scrollToBottom]);

  // Setup content load handler for dynamic content (images, etc.)
  useEffect(() => {
    handleContentLoad();
  }, [messages, handleContentLoad]);

  // Load earlier messages when scrolling to top
  useEffect(() => {
    const chatArea = chatAreaRef.current;
    if (!chatArea) return;

    const handleScroll = () => {
      // Update scroll state on every scroll event
      scrollStateRef.current = {
        scrollHeight: chatArea.scrollHeight,
        scrollTop: chatArea.scrollTop,
      };

      const scrollTop = chatArea.scrollTop;
      const scrollThreshold = 100; // Load when within 100px of top

      if (scrollTop <= scrollThreshold && hasMoreMessagesState && !isPriorSessionLoading) {
        console.log('Scroll event - loading earlier messages');
        loadEarlierMessages();
      }
    };

    chatArea.addEventListener('scroll', handleScroll);
    return () => {
      chatArea.removeEventListener('scroll', handleScroll);
    };
  }, [chatAreaRef, hasMoreMessagesState, isPriorSessionLoading, loadEarlierMessages]);

  // Auto-load earlier messages if viewport isn't filled
  useEffect(() => {
    const chatArea = chatAreaRef.current;
    if (!chatArea || !hasMoreMessagesState || isPriorSessionLoading) return;

    // Check if content height is less than viewport height
    const hasScroll = chatArea.scrollHeight > chatArea.clientHeight;
    if (!hasScroll && messages.length > 0) {
      console.log('Viewport not filled - auto-loading earlier messages');
      loadEarlierMessages();
    }
  }, [chatAreaRef, messages.length, hasMoreMessagesState, isPriorSessionLoading, loadEarlierMessages]);

  // Check if this is a temporary session and show notice if needed
  useEffect(() => {
    // Show notice for temporary session URLs: /temp, /w/:workspaceId/temp, or /.xxx sessions
    const isTempURL = isNewTemporarySessionURL(location.pathname);
    const isTempID = sessionId && sessionId.startsWith('.');

    setShowTempSessionNotice(isTempURL || !!isTempID);
  }, [sessionId, location.pathname]);

  const handleDragOver = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);
  };

  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);

    // Check if this is a message file being dropped
    const dragData = e.dataTransfer.getData('application/json');
    if (dragData) {
      try {
        const parsed = JSON.parse(dragData);
        if (parsed.isMessageAttachment || parsed.isExistingAttachment) {
          // Message files dropped on ChatArea should do nothing
          return;
        }
      } catch {
        // If parsing fails, continue with normal file handling
      }
    }

    // Only handle external files dropped on ChatArea
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      onFilesSelected(Array.from(e.dataTransfer.files));
    }
  };

  // Helper function to check if a message is retryable (last consecutive error messages)
  const isRetryableError = (message: ChatMessageType, allMessages: ChatMessageType[]): boolean => {
    if (message.type !== 'model_error') return false;

    // Check if this message is part of the last consecutive error messages
    for (let i = allMessages.length - 1; i >= 0; i--) {
      const msg = allMessages[i];
      if (msg.type === 'model_error') {
        if (msg.id === message.id) {
          // Found the message in the consecutive error block
          return true;
        }
      } else {
        // Found non-error message, stop
        break;
      }
    }

    return false;
  };

  const renderMessageOrGroup = (
    currentMessage: ChatMessageType,
    messages: ChatMessageType[],
    currentIndex: number,
    availableModels: Map<string, { maxTokens: number }>,
    handleEditMessage: (originalMessageId: string, editedText: string) => Promise<void>,
    handleBranchSwitch: (newBranchId: string) => Promise<void>,
    handleRetryError?: (errorMessageId: string) => Promise<void>,
    mostRecentUserMessageId?: string | null,
  ): { element: JSX.Element; messagesConsumed: number } => {
    // Find maxTokens for the current message's model
    const currentModelMaxTokens = currentMessage.model
      ? availableModels.get(currentMessage.model)?.maxTokens
      : undefined;

    const isLastMessage = currentIndex === messages.length - 1;

    // Check for function_call followed by function_response
    if (
      currentMessage.type === 'function_call' &&
      currentMessage.parts &&
      currentMessage.parts.length > 0 &&
      currentMessage.parts[0].functionCall &&
      currentIndex + 1 < messages.length &&
      messages[currentIndex + 1].type === 'function_response' &&
      messages[currentIndex + 1].parts &&
      messages[currentIndex + 1].parts.length > 0 &&
      messages[currentIndex + 1].parts[0].functionResponse
    ) {
      const functionCall = currentMessage.parts[0].functionCall!;
      const functionResponse = messages[currentIndex + 1].parts[0].functionResponse!;

      // Create messageInfo for the function call message
      const callMessageInfoComponent = (
        <MessageInfo
          cumulTokenCount={currentMessage.cumulTokenCount}
          possibleBranches={currentMessage.possibleBranches}
          model={currentMessage.model}
          maxTokens={availableModels.get(currentMessage.model || '')?.maxTokens}
        />
      );

      // Create messageInfo for the function response message
      const responseMessage = messages[currentIndex + 1];
      const responseMessageInfoComponent = (
        <MessageInfo
          cumulTokenCount={responseMessage.cumulTokenCount}
          possibleBranches={responseMessage.possibleBranches}
          model={responseMessage.model}
          maxTokens={availableModels.get(responseMessage.model || '')?.maxTokens}
        />
      );

      return {
        element: (
          <>
            <FunctionPairMessage
              key={`function-pair-${currentMessage.id}-${messages[currentIndex + 1].id}`}
              functionCall={functionCall}
              functionResponse={functionResponse}
              callMessageId={currentMessage.id}
              responseMessageId={messages[currentIndex + 1].id}
              callMessageInfo={callMessageInfoComponent}
              responseMessageInfo={responseMessageInfoComponent}
              responseAttachments={messages[currentIndex + 1].attachments}
              sessionId={currentMessage.sessionId}
            />
            {isLastMessage && <ProcessingIndicator isLastThoughtGroup={false} isLastModelMessage={true} />}
          </>
        ),
        messagesConsumed: 2,
      };
    } else if (currentMessage.type === 'thought') {
      const thoughtGroup: ChatMessageType[] = [];
      let j = currentIndex;
      while (j < messages.length && messages[j].type === 'thought') {
        thoughtGroup.push(messages[j]);
        j++;
      }

      // Check if this thought group is the very last message(s)
      const isLastThoughtGroup = j === messages.length;

      return {
        element: (
          <ThoughtGroup
            key={`thought-group-${currentIndex}`}
            groupId={`thought-group-${currentIndex}`}
            isAutoDisplayMode={true}
            thoughts={thoughtGroup}
            isLastThoughtGroup={isLastThoughtGroup}
          />
        ),
        messagesConsumed: j - currentIndex,
      };
    } else {
      const isLastModelMessage =
        isLastMessage && (['model', 'model_error', 'error'] as (string | undefined)[]).includes(currentMessage.type);
      return {
        element: (
          <>
            <ChatMessage
              key={currentMessage.id}
              message={currentMessage}
              maxTokens={currentModelMaxTokens}
              isLastModelMessage={isLastModelMessage}
              onSaveEdit={handleEditMessage}
              onRetryClick={handleRetryMessage ? (messageId) => handleRetryMessage(messageId) : undefined}
              onRetryError={
                handleRetryError && isRetryableError(currentMessage, messages)
                  ? (errorMessageId) => handleRetryError(errorMessageId)
                  : undefined
              }
              onBranchSelect={handleBranchSwitch}
              isMostRecentUserMessage={currentMessage.id === mostRecentUserMessageId}
            />
          </>
        ),
        messagesConsumed: 1,
      };
    }
  };

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0; // This will become our startIndex

    // Find the most recent user message ID for accesskey shortcuts
    let mostRecentUserMessageId: string | null = null;
    for (let j = messages.length - 1; j >= 0; j--) {
      if (messages[j].type === 'user') {
        mostRecentUserMessageId = messages[j].id;
        break;
      }
    }

    // Calculate the starting index, skipping incomplete groups at the beginning
    while (i < messages.length) {
      const currentMessage = messages[i];

      // Case 1: Incomplete FunctionPairMessage (function_response without preceding function_call)
      if (currentMessage.type === 'function_response') {
        i++; // Skip this message
        continue;
      }

      // Case 2: Incomplete ThoughtGroup (thought message that's a continuation of a group not fully loaded)
      // If the first message is a thought, we assume it's incomplete and skip it.
      if (currentMessage.type === 'thought') {
        i++; // Skip this message
        continue;
      }

      // If we reach here, the current message is either a complete group or a standalone message
      // that can be rendered, or it's a thought message that's not at the very beginning (meaning it's part of a group that started within the current view).
      // So, we break the loop and start rendering from this 'i'.
      break;
    }

    // Now, render messages from the calculated startIndex
    while (i < messages.length) {
      const currentMessage = messages[i];
      const { element, messagesConsumed } = renderMessageOrGroup(
        currentMessage,
        messages,
        i,
        availableModels,
        handleEditMessage,
        handleBranchSwitch,
        handleRetryError,
        mostRecentUserMessageId,
      );
      renderedElements.push(element);
      i += messagesConsumed;
    }

    // Add temporary environment change message if it exists
    if (temporaryEnvChangeMessage) {
      renderedElements.push(
        <ChatMessage
          key={temporaryEnvChangeMessage.id}
          message={temporaryEnvChangeMessage}
          maxTokens={undefined} // Temporary messages don't have token limits
          isLastModelMessage={false}
          onSaveEdit={() => {}}
          onRetryClick={handleRetryMessage ? (messageId) => handleRetryMessage(messageId) : undefined}
          onRetryError={handleRetryError ? (errorMessageId) => handleRetryError(errorMessageId) : undefined}
          onBranchSelect={handleBranchSwitch}
          isMostRecentUserMessage={false}
        />,
      );
    }

    // Check if we need to render ProcessingIndicator outside message list
    const shouldRenderOutsideProcessingIndicator = () => {
      if (!startTime) return false;

      // If there are no messages, render outside
      if (messages.length === 0) return true;

      // Check if the last message is a model message or thought
      const lastMessage = messages[messages.length - 1];
      return (
        !lastMessage.type || !(['model', 'model_error', 'error', 'thought'] as string[]).includes(lastMessage.type)
      );
    };

    // Add ProcessingIndicator outside message list when needed
    if (shouldRenderOutsideProcessingIndicator()) {
      renderedElements.push(
        <ProcessingIndicator key="processing-indicator" isLastThoughtGroup={false} isLastModelMessage={false} />,
      );
    }

    return renderedElements;
  }, [
    messages,
    availableModels,
    startTime,
    temporaryEnvChangeMessage,
    showTempSessionNotice,
    hasMoreMessagesState,
    handleEditMessage,
    handleBranchSwitch,
    handleRetryMessage,
    handleRetryError,
  ]);

  const currentSystemPromptLabel = useMemo(() => {
    const found = globalPrompts.find((p) => p.value === systemPrompt);
    return found ? found.label : ''; // Return label if found, else empty string for custom
  }, [systemPrompt, globalPrompts]);

  return (
    <div
      style={{
        flexGrow: 1,
        width: '0',
        display: 'flex',
        flexDirection: 'column',
        position: 'relative',
      }}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {isDragging && (
        <div
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 123, 255, 0.1)',
            border: '2px solid rgba(0, 123, 255, 0.3)',
            pointerEvents: 'none',
            zIndex: 10,
          }}
        />
      )}
      <div style={{ flexGrow: 1, overflowY: 'auto' }} ref={chatAreaRef}>
        <div
          style={{
            maxWidth: 'var(--chat-container-max-width)',
            margin: '0 auto',
            padding: 'var(--spacing-unit)',
          }}
        >
          {!hasMoreMessagesState && (
            <>
              {showTempSessionNotice && (
                <div
                  key="temp-session-notice"
                  style={{
                    textAlign: 'center',
                    padding: '20px',
                    margin: '0 0 20px 0',
                    backgroundColor: '#fff3cd',
                    border: '1px solid #ffeaa7',
                    borderRadius: '8px',
                    color: '#856404',
                    fontSize: '16px',
                    fontWeight: '500',
                  }}
                >
                  This is a temporary session, to be deleted after 48 hours of inactivity.
                </div>
              )}
              <SystemPromptEditor
                key={sessionId || 'new'}
                initialPrompt={systemPrompt}
                currentLabel={currentSystemPromptLabel}
                onPromptUpdate={(updatedPrompt) => {
                  setSystemPrompt(updatedPrompt.value);
                }}
                isEditing={isSystemPromptEditing}
                predefinedPrompts={globalPrompts}
                workspaceId={workspaceId || undefined}
              />
            </>
          )}

          {isPriorSessionLoading && (
            <div style={{ textAlign: 'center', padding: '10px' }}>Loading more messages...</div>
          )}
          {renderedMessages}
          <div ref={messagesEndRef} />
        </div>
      </div>
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
    </div>
  );
};

export default ChatArea;
