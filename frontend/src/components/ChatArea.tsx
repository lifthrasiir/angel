import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useAtom, useAtomValue } from 'jotai';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import ChatInput from './ChatInput';
import TokenCountMeter from './TokenCountMeter';
import ChatMessage from './ChatMessage';
import FileAttachmentPreview from './FileAttachmentPreview';
import SystemPromptEditor from './SystemPromptEditor';
import { ThoughtGroup } from './ThoughtGroup';
import FunctionPairMessage from './FunctionPairMessage';
import {
  messagesAtom,
  chatSessionIdAtom,
  selectedFilesAtom,
  availableModelsAtom,
  userEmailAtom,
  systemPromptAtom,
  isSystemPromptEditingAtom,
  globalPromptsAtom,
  workspaceIdAtom,
  processingStartTimeAtom,
  primaryBranchIdAtom,
  isPriorSessionLoadingAtom,
  hasMoreMessagesAtom,
  isPriorSessionLoadCompleteAtom,
} from '../atoms/chatAtoms';
import { ProcessingIndicator } from './ProcessingIndicator';
import MessageInfo from './MessageInfo';
import { useSessionLoader } from '../hooks/useSessionLoader';

interface ChatAreaProps {
  handleSendMessage: () => void;
  onFilesSelected: (files: File[]) => void;
  handleRemoveFile: (index: number) => void;
  handleCancelStreaming: () => void;
  chatInputRef: React.RefObject<HTMLTextAreaElement>;
  chatAreaRef: React.RefObject<HTMLDivElement>;
}

const ChatArea: React.FC<ChatAreaProps> = ({
  handleSendMessage,
  onFilesSelected,
  handleRemoveFile,
  handleCancelStreaming,
  chatInputRef,
  chatAreaRef,
}) => {
  const [workspaceId] = useAtom(workspaceIdAtom);
  const [messages] = useAtom(messagesAtom);
  const [chatSessionId] = useAtom(chatSessionIdAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [userEmail] = useAtom(userEmailAtom);
  const [systemPrompt, setSystemPrompt] = useAtom(systemPromptAtom);
  const isSystemPromptEditing = useAtomValue(isSystemPromptEditingAtom);
  const [globalPrompts] = useAtom(globalPromptsAtom);
  const processingStartTime = useAtomValue(processingStartTimeAtom);
  const primaryBranchId = useAtomValue(primaryBranchIdAtom);
  const { loadMoreMessages } = useSessionLoader({ chatSessionId, primaryBranchId, chatAreaRef });
  const hasMoreMessages = useAtomValue(hasMoreMessagesAtom);
  const isPriorSessionLoading = useAtomValue(isPriorSessionLoadingAtom);
  const isPriorSessionLoadComplete = useAtomValue(isPriorSessionLoadCompleteAtom);
  const [isDragging, setIsDragging] = useState(false); // State for drag and drop

  const isLoggedIn = !!userEmail;

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const prevMessagesLengthRef = useRef(messages.length);
  const prevIsPriorSessionLoadingRef = useRef(isPriorSessionLoading); // New ref

  useEffect(() => {
    const wasLoadingPrior = prevIsPriorSessionLoadingRef.current; // Get previous state

    // Only scroll to bottom if:
    // 1. New messages are added to the end (messages.length increased)
    // AND
    // 2. We are NOT currently loading prior session messages (!isPriorSessionLoading)
    // AND
    // 3. We just finished loading prior session messages (wasLoadingPrior is true and isPriorSessionLoading is false)
    //    OR we were never loading prior session messages (wasLoadingPrior is false)
    // This complex condition aims to prevent scrolling to bottom when prior messages just finished loading.
    if (
      messages.length > prevMessagesLengthRef.current &&
      !isPriorSessionLoading &&
      !(wasLoadingPrior && !isPriorSessionLoading)
    ) {
      messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
    }

    prevMessagesLengthRef.current = messages.length;
    prevIsPriorSessionLoadingRef.current = isPriorSessionLoading; // Update ref
  }, [messages, isPriorSessionLoading]);

  useEffect(() => {
    const chatAreaElement = chatAreaRef.current;
    if (!chatAreaElement) {
      return;
    }

    const handleScroll = () => {
      if (chatAreaElement.scrollTop === 0 && !isPriorSessionLoading && hasMoreMessages && isPriorSessionLoadComplete) {
        loadMoreMessages();
      }
    };

    chatAreaElement.addEventListener('scroll', handleScroll);

    return () => {
      chatAreaElement.removeEventListener('scroll', handleScroll);
    };
  }, [chatAreaRef, isPriorSessionLoading, loadMoreMessages, isPriorSessionLoadComplete]);

  useEffect(() => {
    const chatAreaElement = chatAreaRef.current;
    if (chatAreaElement && hasMoreMessages && !isPriorSessionLoading) {
      // console.log(`Initial load check: scrollHeight: ${chatAreaElement.scrollHeight}, clientHeight: ${chatAreaElement.clientHeight}`);
      // Check if the content height is less than the visible height (no scrollbar)
      // This indicates that all available messages might not have been loaded yet,
      // especially in short sessions where initial messages don't fill the viewport.
      if (chatAreaElement.scrollHeight <= chatAreaElement.clientHeight) {
        loadMoreMessages();
      }
    }
  }, [chatAreaRef, hasMoreMessages, isPriorSessionLoading, loadMoreMessages]);

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
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      onFilesSelected(Array.from(e.dataTransfer.files));
    }
  };

  const renderMessageOrGroup = (
    currentMessage: ChatMessageType,
    messages: ChatMessageType[],
    currentIndex: number,
    availableModels: Map<string, { maxTokens: number }>,
    processingStartTime: number | null,
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
          branchId={currentMessage.branchId}
          parentMessageId={currentMessage.parentMessageId}
          chosenNextId={currentMessage.chosenNextId}
          possibleNextIds={currentMessage.possibleNextIds}
          model={currentMessage.model}
          maxTokens={availableModels.get(currentMessage.model || '')?.maxTokens}
        />
      );

      // Create messageInfo for the function response message
      const responseMessage = messages[currentIndex + 1];
      const responseMessageInfoComponent = (
        <MessageInfo
          cumulTokenCount={responseMessage.cumulTokenCount}
          branchId={responseMessage.branchId}
          parentMessageId={responseMessage.parentMessageId}
          chosenNextId={responseMessage.chosenNextId}
          possibleNextIds={responseMessage.possibleNextIds}
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
            />
            {isLastMessage && processingStartTime !== null && (
              <ProcessingIndicator
                startTime={processingStartTime}
                isLastThoughtGroup={false}
                isLastModelMessage={false} // This case is not a model message
              />
            )}
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
            processingStartTime={processingStartTime}
          />
        ),
        messagesConsumed: j - currentIndex,
      };
    } else {
      const isLastModelMessage = isLastMessage && currentMessage.role === 'model';
      return {
        element: (
          <>
            <ChatMessage
              key={currentMessage.id}
              message={currentMessage}
              maxTokens={currentModelMaxTokens}
              isLastModelMessage={isLastModelMessage}
              processingStartTime={processingStartTime}
            />
            {isLastMessage && processingStartTime !== null && !isLastModelMessage && (
              <ProcessingIndicator
                startTime={processingStartTime!}
                isLastThoughtGroup={false}
                isLastModelMessage={false}
              />
            )}
            {/* For model messages, the indicator is now rendered inside ChatMessage/ModelTextMessage */}
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
        processingStartTime,
      );
      renderedElements.push(element);
      i += messagesConsumed;
    }
    return renderedElements;
  }, [messages, availableModels, globalPrompts, systemPrompt, processingStartTime]);

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
        border: isDragging ? '2px dashed #007bff' : '2px dashed transparent',
        transition: 'border-color 0.3s ease-in-out',
      }}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {!isLoggedIn && (
        <div style={{ padding: '20px', textAlign: 'center' }}>
          <p>Login required to start chatting.</p>
        </div>
      )}

      {isLoggedIn && (
        <>
          <div style={{ flexGrow: 1, overflowY: 'auto' }} ref={chatAreaRef}>
            <div style={{ maxWidth: '60em', margin: '0 auto', padding: '20px' }}>
              {!hasMoreMessages && (
                <>
                  <div style={{ textAlign: 'center', padding: '10px', color: '#888' }}>
                    No more messages. This is the beginning of the session.
                  </div>
                  <SystemPromptEditor
                    key={chatSessionId}
                    initialPrompt={systemPrompt}
                    currentLabel={currentSystemPromptLabel}
                    onPromptUpdate={(updatedPrompt) => {
                      setSystemPrompt(updatedPrompt.value);
                    }}
                    isEditing={isSystemPromptEditing}
                    predefinedPrompts={globalPrompts}
                    workspaceId={workspaceId}
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
          {selectedFiles.length > 0 && (
            <div
              style={{
                padding: '5px 20px',
                borderTop: '1px solid #eee',
                background: '#f9f9f9',
                display: 'flex',
                flexWrap: 'wrap',
                gap: '5px',
              }}
            >
              {selectedFiles.map((file, index) => (
                <FileAttachmentPreview key={index} file={file} onRemove={() => handleRemoveFile(index)} />
              ))}
            </div>
          )}
          <TokenCountMeter />
          <ChatInput
            handleSendMessage={handleSendMessage}
            onFilesSelected={onFilesSelected}
            handleCancelStreaming={handleCancelStreaming}
            inputRef={chatInputRef}
            sessionId={chatSessionId}
          />
        </>
      )}
    </div>
  );
};

export default ChatArea;
