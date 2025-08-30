import type React from 'react';
import type { FileAttachment } from '../types/chat';
import FileAttachmentList from './FileAttachmentList';
import ChatBubble from './ChatBubble';

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[];
  messageInfo?: React.ReactNode;
  messageId?: string;
  sessionId?: string;
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({ text, attachments, messageInfo, messageId, sessionId }) => {
  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="user-message-bubble-content"
      messageInfo={messageInfo}
      heighten={false}
    >
      {text}
      <FileAttachmentList attachments={attachments} messageId={messageId} sessionId={sessionId} />
    </ChatBubble>
  );
};

export default UserTextMessage;
