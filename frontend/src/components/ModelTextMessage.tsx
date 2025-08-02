import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode; // New prop for MessageInfo
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({ text, className, messageInfo }) => {
  return (
    <div className={`chat-message-container ${className || ''}`}>
      <div className="chat-bubble">
        <MarkdownRenderer content={text || ''} />
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default ModelTextMessage;
