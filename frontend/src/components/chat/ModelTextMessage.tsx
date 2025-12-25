import React from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import { ProcessingIndicator } from './ProcessingIndicator';
import ChatBubble from './ChatBubble';
import { FileAttachment, ChatMessage } from '../../types/chat';
import FileAttachmentList from '../FileAttachmentList';
import MessageInfo from './MessageInfo';
import { isImageOnlyMessage } from '../../utils/messageUtils';
import CopyButton from './CopyButton';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  isLastModelMessage?: boolean;
  messageId?: string;
  attachments?: FileAttachment[];
  sessionId?: string;
  sideContents?: React.ReactNode;
  message?: ChatMessage;
  isMobile?: boolean;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({
  text,
  className,
  messageInfo,
  isLastModelMessage,
  messageId,
  attachments,
  sideContents,
  message,
  isMobile = false,
}) => {
  const imageOnly = isImageOnlyMessage(text, attachments);

  // If there's already sideContents (e.g., RetryErrorButton), use it; otherwise add CopyButton
  const finalSideContents = sideContents || (text && <CopyButton text={text} />);

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName={className}
      messageInfo={
        React.isValidElement(messageInfo) && messageInfo.type === MessageInfo
          ? React.cloneElement(messageInfo, {
              message: message,
              isMobile: isMobile,
            } as any)
          : messageInfo
      }
      sideContents={finalSideContents}
    >
      <FileAttachmentList attachments={attachments} isImageOnlyMessage={imageOnly} />
      {!imageOnly && <MarkdownRenderer content={text || ''} />}
      {isLastModelMessage && <ProcessingIndicator isLastThoughtGroup={false} isLastModelMessage={true} />}
    </ChatBubble>
  );
};

export default ModelTextMessage;
