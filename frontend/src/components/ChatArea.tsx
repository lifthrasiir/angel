import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useAtom } from 'jotai';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import ChatInput from './ChatInput';
import ChatMessage from './ChatMessage';
import FileAttachmentPreview from './FileAttachmentPreview';
import SystemPromptEditor from './SystemPromptEditor';
import { ThoughtGroup } from './ThoughtGroup';
import FunctionPairMessage from './FunctionPairMessage';
import {
  messagesAtom,
  lastAutoDisplayedThoughtIdAtom,
  chatSessionIdAtom,
  selectedFilesAtom,
  availableModelsAtom,
  userEmailAtom,
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
  const [messages] = useAtom(messagesAtom);
  const [lastAutoDisplayedThoughtId] = useAtom(lastAutoDisplayedThoughtIdAtom);
  const [chatSessionId] = useAtom(chatSessionIdAtom);
  const [selectedFiles] = useAtom(selectedFilesAtom);
  const [availableModels] = useAtom(availableModelsAtom);
  const [userEmail] = useAtom(userEmailAtom);
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

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0;
    while (i < messages.length) {
      const currentMessage = messages[i];

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
        i + 1 < messages.length &&
        messages[i + 1].type === 'function_response' &&
        messages[i + 1].parts &&
        messages[i + 1].parts.length > 0 &&
        messages[i + 1].parts[0].functionResponse
      ) {
        const functionCall = currentMessage.parts[0].functionCall!;
        const functionResponse = messages[i + 1].parts[0].functionResponse!;
        renderedElements.push(
          <FunctionPairMessage
            key={`function-pair-${currentMessage.id}-${messages[i + 1].id}`}
            functionCall={functionCall}
            functionResponse={functionResponse}
          />,
        );
        i += 2; // Skip both messages
      } else if (currentMessage.type === 'thought') {
        const thoughtGroup: ChatMessageType[] = [];
        let j = i;
        while (j < messages.length && messages[j].type === 'thought') {
          thoughtGroup.push(messages[j]);
          j++;
        }
        renderedElements.push(
          <ThoughtGroup
            key={`thought-group-${i}`}
            groupId={`thought-group-${i}`}
            isAutoDisplayMode={true}
            thoughts={thoughtGroup}
          />,
        );
        i = j; // Move index past the grouped thoughts
      } else {
        renderedElements.push(
          <ChatMessage
            key={currentMessage.id}
            message={currentMessage}
            maxTokens={currentModelMaxTokens} // Pass maxTokens to ChatMessage
          />,
        );
        i++;
      }
    }
    return renderedElements;
  }, [messages, lastAutoDisplayedThoughtId, availableModels]);

  return (
    <div
      style={{
        flexGrow: 1,
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
              <SystemPromptEditor key={chatSessionId} />
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
          <ChatInput
            handleSendMessage={handleSendMessage}
            onFilesSelected={onFilesSelected}
            handleCancelStreaming={handleCancelStreaming}
            inputRef={chatInputRef}
          />
        </>
      )}
    </div>
  );
};

export default ChatArea;
