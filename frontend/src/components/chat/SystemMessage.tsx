import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import ChatBubble from './ChatBubble';

interface SystemMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  messageId?: string;
}

const SystemMessage: React.FC<SystemMessageProps> = ({ text, className, messageInfo, messageId }) => {
  return (
    <ChatBubble
      messageId={messageId}
      containerClassName={`system-message ${className || ''}`}
      messageInfo={messageInfo}
    >
      <MarkdownRenderer content={text || ''} />
    </ChatBubble>
  );
};

export default SystemMessage;
