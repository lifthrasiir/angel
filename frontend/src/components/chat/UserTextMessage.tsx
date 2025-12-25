import React, { useRef } from 'react';
import { useAtom } from 'jotai';
import type { FileAttachment, ChatMessage } from '../../types/chat';
import FileAttachmentList from '../FileAttachmentList';
import ChatBubble, { type ChatBubbleRef } from './ChatBubble';
import { editingMessageIdAtom } from '../../atoms/chatAtoms';
import { useProcessingState } from '../../hooks/useProcessingState';
import type { MessageInfoProps } from './MessageInfo';
import MessageInfo from './MessageInfo';
import { isImageOnlyMessage } from '../../utils/messageUtils';
import CopyButton from './CopyButton';
import './UserTextMessage.css';

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[];
  messageInfo?: React.ReactElement<MessageInfoProps>; // Change type here
  messageId?: string;
  sessionId?: string;
  message?: ChatMessage;
  onSaveEdit: (messageId: string, editedText: string) => void;
  onRetryClick?: (messageId: string) => void;
  isMobile?: boolean;
  isMostRecentUserMessage?: boolean;
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({
  text,
  attachments,
  messageInfo,
  messageId,
  message,
  onSaveEdit,
  onRetryClick,
  isMobile = false,
  isMostRecentUserMessage = false,
}) => {
  const { isProcessing } = useProcessingState();
  const [editingMessageId, setEditingMessageId] = useAtom(editingMessageIdAtom);
  const chatBubbleRef = useRef<ChatBubbleRef>(null);

  const isEditing = messageId === editingMessageId;
  const imageOnly = isImageOnlyMessage(text, attachments);

  const handleEditClick = () => {
    if (!isProcessing) {
      setEditingMessageId(messageId || null);
    }
  };

  const handleEditSave = (newText: string) => {
    if (messageId) {
      onSaveEdit(messageId, newText);
    }
    setEditingMessageId(null);
  };

  const handleRetry = () => {
    if (onRetryClick && messageId) {
      onRetryClick(messageId);
    }
  };

  const handleEditCancel = () => {
    setEditingMessageId(null);
  };

  return (
    <ChatBubble
      ref={chatBubbleRef}
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="user-message-bubble-content"
      messageInfo={
        React.isValidElement(messageInfo) && messageInfo.type === MessageInfo // Add type check
          ? React.cloneElement(messageInfo, {
              onEditClick: handleEditClick,
              onRetryClick: handleRetry,
              isEditing: isEditing && !imageOnly,
              onEditSave: () => {
                if (chatBubbleRef.current) {
                  chatBubbleRef.current.saveEdit();
                }
              },
              onEditCancel: handleEditCancel,
              message: message,
              isMobile: isMobile,
              editAccessKey: isMostRecentUserMessage ? 'e' : undefined,
              retryAccessKey: isMostRecentUserMessage ? 'r' : undefined,
            } as Partial<MessageInfoProps>) // Cast to Partial<MessageInfoProps>
          : messageInfo
      }
      heighten={false}
      editText={!imageOnly ? text : ''}
      isEditing={isEditing && !imageOnly}
      onEditSave={handleEditSave}
      onEditCancel={handleEditCancel}
      sideContents={text && <CopyButton text={text} />}
    >
      <>
        {!imageOnly && text}
        <FileAttachmentList attachments={attachments} isImageOnlyMessage={imageOnly} />
      </>
    </ChatBubble>
  );
};

export default UserTextMessage;
