import React, { useRef, useEffect, useMemo } from 'react';
import ChatMessage from './ChatMessage';
import { ThoughtGroup } from './ThoughtGroup';
import SystemPromptEditor from './SystemPromptEditor';
import ChatInput from './ChatInput';

interface ChatMessage {
  id: string;
  role: string;
  parts: { text?: string; functionCall?: any; functionResponse?: any; }[];
  type?: "model" | "thought" | "system" | "user" | "function_call" | "function_response";
}

interface ChatAreaProps {
  isLoggedIn: boolean;
  messages: ChatMessage[];
  lastAutoDisplayedThoughtId: string | null;
  systemPrompt: string;
  setSystemPrompt: React.Dispatch<React.SetStateAction<string>>;
  isSystemPromptEditing: boolean;
  setIsSystemPromptEditing: React.Dispatch<React.SetStateAction<boolean>>;
  inputMessage: string;
  setInputMessage: React.Dispatch<React.SetStateAction<string>>;
  handleSendMessage: () => void;
  isStreaming: boolean;
}

const ChatArea: React.FC<ChatAreaProps> = ({
  isLoggedIn,
  messages,
  lastAutoDisplayedThoughtId,
  systemPrompt,
  setSystemPrompt,
  isSystemPromptEditing,
  setIsSystemPromptEditing,
  inputMessage,
  setInputMessage,
  handleSendMessage,
  isStreaming,
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
        const thoughtGroup: ChatMessage[] = [];
        let j = i;
        while (j < messages.length && messages[j].type === 'thought') {
          thoughtGroup.push(messages[j]);
          j++;
        }
        renderedElements.push(<ThoughtGroup key={`thought-group-${i}`} groupId={`thought-group-${i}`} thoughts={thoughtGroup} isAutoDisplayMode={true} lastAutoDisplayedThoughtId={lastAutoDisplayedThoughtId} />);
        i = j; // Move index past the grouped thoughts
      } else {
        renderedElements.push(
          <ChatMessage
            key={currentMessage.id}
            role={currentMessage.role}
            text={currentMessage.parts[0].text}
            type={currentMessage.type}
            functionCall={currentMessage.parts[0].functionCall}
            functionResponse={currentMessage.parts[0].functionResponse}
          />
        );
        i++;
      }
    }
    return renderedElements;
  }, [messages, lastAutoDisplayedThoughtId]);

  return (
    <div style={{ flexGrow: 1, display: 'flex', flexDirection: 'column', position: 'relative' }}>
      {!isLoggedIn && (
        <div style={{ padding: '20px', textAlign: 'center' }}>
          <p>Login required to start chatting.</p>
        </div>
      )}
      
      {isLoggedIn && (
        <>
          <div style={{ flexGrow: 1, overflowY: 'auto' }}>
            <div style={{ maxWidth: '60em', margin: '0 auto', padding: '20px' }}>
              <SystemPromptEditor
                systemPrompt={systemPrompt}
                setSystemPrompt={setSystemPrompt}
                isSystemPromptEditing={isSystemPromptEditing}
                setIsSystemPromptEditing={setIsSystemPromptEditing}
                messagesLength={messages.length}
              />
              {renderedMessages}
              <div ref={messagesEndRef} />
            </div>
          </div>
          <ChatInput
            inputMessage={inputMessage}
            setInputMessage={setInputMessage}
            handleSendMessage={handleSendMessage}
            isStreaming={isStreaming}
          />
        </>
      )}
    </div>
  );
};

export default ChatArea;
