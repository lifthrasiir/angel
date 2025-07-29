import React from 'react';
import 'katex/dist/katex.min.css';
import MarkdownRenderer from './MarkdownRenderer';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({ text, className }) => {
  return (
    <div className="chat-message-container agent-message">
      <div className={`chat-bubble ${className || ''}`}>
        <MarkdownRenderer content={text || ''} />
      </div>
    </div>
  );
};

export default ModelTextMessage;