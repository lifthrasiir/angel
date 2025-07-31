import React from 'react';
import MarkdownRenderer from './MarkdownRenderer';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({ text, className }) => {
  return (
    <div className={`chat-message-container ${className || ''}`}>
      <div className="chat-bubble">
        <MarkdownRenderer content={text || ''} />
      </div>
    </div>
  );
};

export default ModelTextMessage;
