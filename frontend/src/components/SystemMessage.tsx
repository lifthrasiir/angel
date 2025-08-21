import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';

interface SystemMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  messageId?: string; // Add messageId prop
}

const SystemMessage: React.FC<SystemMessageProps> = ({ text, className, messageInfo, messageId }) => {
  return (
    <div id={messageId} className={`chat-message-container system-message ${className || ''}`}>
      <div className="chat-bubble">
        <MarkdownRenderer content={text || ''} />
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default SystemMessage;
