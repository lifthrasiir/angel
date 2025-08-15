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
} from '../atoms/chatAtoms';

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
  const [isDragging, setIsDragging] = useState(false); // State for drag and drop

  const isLoggedIn = !!userEmail;

  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

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
  ): { element: JSX.Element; messagesConsumed: number } => {
    // Find maxTokens for the current message's model
    const currentModelMaxTokens = currentMessage.model
      ? availableModels.get(currentMessage.model)?.maxTokens
      : undefined;

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
      return {
        element: (
          <FunctionPairMessage
            key={`function-pair-${currentMessage.id}-${messages[currentIndex + 1].id}`}
            functionCall={functionCall}
            functionResponse={functionResponse}
          />
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
      return {
        element: (
          <ThoughtGroup
            key={`thought-group-${currentIndex}`}
            groupId={`thought-group-${currentIndex}`}
            isAutoDisplayMode={true}
            thoughts={thoughtGroup}
          />
        ),
        messagesConsumed: j - currentIndex,
      };
    } else {
      return {
        element: (
          <ChatMessage
            key={currentMessage.id}
            message={currentMessage}
            maxTokens={currentModelMaxTokens} // Pass maxTokens to ChatMessage
          />
        ),
        messagesConsumed: 1,
      };
    }
  };

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0;
    while (i < messages.length) {
      const currentMessage = messages[i];
      const { element, messagesConsumed } = renderMessageOrGroup(currentMessage, messages, i, availableModels);
      renderedElements.push(element);
      i += messagesConsumed;
    }
    return renderedElements;
  }, [messages, availableModels, globalPrompts, systemPrompt]);

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
              <SystemPromptEditor
                key={chatSessionId}
                initialPrompt={systemPrompt}
                currentLabel={currentSystemPromptLabel} // Pass the derived label
                onPromptUpdate={(updatedPrompt) => {
                  setSystemPrompt(updatedPrompt.value); // Only update the value of systemPrompt atom
                }}
                isEditing={isSystemPromptEditing}
                predefinedPrompts={globalPrompts}
                workspaceId={workspaceId} // Pass workspaceId
              />
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
