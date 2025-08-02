import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';

interface SystemMessageProps {
  text?: string;
  messageInfo?: React.ReactNode;
}

const SystemMessage: React.FC<SystemMessageProps> = ({ text, messageInfo }) => {
  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble system-prompt-bubble">
        <MarkdownRenderer content={text || ''} />
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default SystemMessage;
