import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import { ProcessingIndicator } from './ProcessingIndicator'; // Import the new component

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode; // New prop for MessageInfo
  isLastModelMessage?: boolean; // New prop
  processingStartTime?: number | null; // New prop
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({
  text,
  className,
  messageInfo,
  isLastModelMessage,
  processingStartTime,
}) => {
  return (
    <div className={`chat-message-container ${className || ''}`}>
      <div className="chat-bubble">
        <MarkdownRenderer content={text || ''} />
        {isLastModelMessage && processingStartTime !== null && (
          <ProcessingIndicator startTime={processingStartTime!} isLastThoughtGroup={false} isLastModelMessage={true} />
        )}
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default ModelTextMessage;
