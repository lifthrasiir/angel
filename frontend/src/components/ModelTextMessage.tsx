import type React from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import { ProcessingIndicator } from './ProcessingIndicator';
import ChatBubble from './ChatBubble';
import { FileAttachment } from '../types/chat';
import FileAttachmentList from './FileAttachmentList';
import { isImageOnlyMessage } from '../utils/messageUtils';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  isLastModelMessage?: boolean;
  processingStartTime?: number | null;
  messageId?: string;
  attachments?: FileAttachment[];
  sessionId?: string;
  sideContents?: React.ReactNode;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({
  text,
  className,
  messageInfo,
  isLastModelMessage,
  processingStartTime,
  messageId,
  attachments,
  sideContents,
}) => {
  const imageOnly = isImageOnlyMessage(text, attachments);

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName={className}
      messageInfo={messageInfo}
      sideContents={sideContents}
    >
      <FileAttachmentList attachments={attachments} isImageOnlyMessage={imageOnly} />
      {!imageOnly && <MarkdownRenderer content={text || ''} />}
      {isLastModelMessage && processingStartTime !== null && (
        <ProcessingIndicator startTime={processingStartTime!} isLastThoughtGroup={false} isLastModelMessage={true} />
      )}
    </ChatBubble>
  );
};

export default ModelTextMessage;
