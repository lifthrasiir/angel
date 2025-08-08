import type React from 'react';
import { useEffect, useMemo, useRef } from 'react';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import ChatInput from './ChatInput';
import ChatMessage from './ChatMessage';
import FileAttachmentPreview from './FileAttachmentPreview';
import SystemPromptEditor from './SystemPromptEditor';
import { ThoughtGroup } from './ThoughtGroup';
import FunctionPairMessage from './FunctionPairMessage';

interface ChatAreaProps {
  isLoggedIn: boolean;
  messages: ChatMessageType[];
  lastAutoDisplayedThoughtId: string | null;
  systemPrompt: string;
  setSystemPrompt: (prompt: string) => void;
  isSystemPromptEditing: boolean;
  chatSessionId: string | null;

  inputMessage: string;
  setInputMessage: (message: string) => void;
  handleSendMessage: () => void;
  isStreaming: boolean;
  onFilesSelected: (files: File[]) => void;
  selectedFiles: File[];
  handleRemoveFile: (index: number) => void;
  handleCancelStreaming: () => void;
  chatInputRef: React.RefObject<HTMLTextAreaElement>;
  chatAreaRef: React.RefObject<HTMLDivElement>;
  availableModels: string[]; // New prop
  selectedModel: string; // New prop
  setSelectedModel: (model: string) => void; // New prop
}

const ChatArea: React.FC<ChatAreaProps> = ({
  isLoggedIn,
  messages,
  lastAutoDisplayedThoughtId,
  systemPrompt,
  setSystemPrompt,
  isSystemPromptEditing,
  chatSessionId,

  inputMessage,
  setInputMessage,
  handleSendMessage,
  isStreaming,
  onFilesSelected,
  selectedFiles,
  handleRemoveFile,
  handleCancelStreaming,
  chatInputRef,
  chatAreaRef,
  availableModels,
  selectedModel,
  setSelectedModel,
}) => {
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // Logic to group consecutive thought messages
  const renderedMessages = useMemo(() => {
    const renderedElements: JSX.Element[] = [];
    let i = 0;
    while (i < messages.length) {
      const currentMessage = messages[i];

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
            thoughts={thoughtGroup}
            isAutoDisplayMode={true}
            lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId}
          />,
        );
        i = j; // Move index past the grouped thoughts
      } else {
        renderedElements.push(<ChatMessage key={currentMessage.id} message={currentMessage} />);
        i++;
      }
    }
    return renderedElements;
  }, [messages, lastAutoDisplayedThoughtId]);

  return (
    <div
      style={{
        flexGrow: 1,
        display: 'flex',
        flexDirection: 'column',
        position: 'relative',
      }}
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
                systemPrompt={systemPrompt}
                setSystemPrompt={setSystemPrompt}
                isSystemPromptEditing={isSystemPromptEditing}
                messagesLength={messages.length}
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
          <ChatInput
            inputMessage={inputMessage}
            setInputMessage={setInputMessage}
            handleSendMessage={handleSendMessage}
            isStreaming={isStreaming}
            onFilesSelected={onFilesSelected}
            handleCancelStreaming={handleCancelStreaming}
            inputRef={chatInputRef}
            availableModels={availableModels}
            selectedModel={selectedModel}
            setSelectedModel={setSelectedModel}
          />
        </>
      )}
    </div>
  );
};

export default ChatArea;
