import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import { ProcessingIndicator } from './ProcessingIndicator';
import ChatBubble from './ChatBubble';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  isLastModelMessage?: boolean;
  processingStartTime?: number | null;
  messageId?: string;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({
  text,
  className,
  messageInfo,
  isLastModelMessage,
  processingStartTime,
  messageId,
}) => {
  return (
    <ChatBubble messageId={messageId} containerClassName={className} messageInfo={messageInfo}>
      <MarkdownRenderer content={text || ''} />
      {isLastModelMessage && processingStartTime !== null && (
        <ProcessingIndicator startTime={processingStartTime!} isLastThoughtGroup={false} isLastModelMessage={true} />
      )}
    </ChatBubble>
  );
};

export default ModelTextMessage;
