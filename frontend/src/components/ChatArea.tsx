import type React from 'react';
import { useEffect, useMemo, useRef } from 'react';
import type { ChatMessage as ChatMessageType } from '../types/chat';
import ChatInput from './ChatInput';
import ChatMessage from './ChatMessage';
import FileAttachmentPreview from './FileAttachmentPreview';
import SystemPromptEditor from './SystemPromptEditor';
import { ThoughtGroup } from './ThoughtGroup';

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
      if (currentMessage.type === 'thought') {
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
          />
        </>
      )}
    </div>
  );
};

export default ChatArea;
