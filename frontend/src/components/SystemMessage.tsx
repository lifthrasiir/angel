import React from 'react';
import 'katex/dist/katex.min.css';
import MarkdownRenderer from './MarkdownRenderer';

interface SystemMessageProps {
  text?: string;
}

const SystemMessage: React.FC<SystemMessageProps> = ({ text }) => {
  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble system-prompt-bubble">
        <MarkdownRenderer content={text || ''} />
      </div>
    </div>
  );
};

export default SystemMessage;