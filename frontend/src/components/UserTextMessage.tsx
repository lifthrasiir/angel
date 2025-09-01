import React, { useState, useEffect, useRef } from 'react';
import { useAtom } from 'jotai';
import type { FileAttachment } from '../types/chat';
import FileAttachmentList from './FileAttachmentList';
import ChatBubble from './ChatBubble';
import { editingMessageIdAtom, processingStartTimeAtom } from '../atoms/chatAtoms';
import type { MessageInfoProps } from './MessageInfo';
import MessageInfo from './MessageInfo';

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[];
  messageInfo?: React.ReactElement<MessageInfoProps>; // Change type here
  messageId?: string;
  sessionId?: string;
  onSaveEdit: (messageId: string, editedText: string) => void;
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({
  text,
  attachments,
  messageInfo,
  messageId,
  sessionId,
  onSaveEdit,
}) => {
  const [editingMessageId, setEditingMessageId] = useAtom(editingMessageIdAtom);
  const [processingStartTime] = useAtom(processingStartTimeAtom);
  const isProcessing = processingStartTime !== null;

  const isEditing = messageId === editingMessageId;
  const [editedText, setEditedText] = useState(text || '');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (isEditing && textareaRef.current) {
      textareaRef.current.focus();
      // Adjust height on initial render if it's in editing mode
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = textareaRef.current.scrollHeight + 'px';
    }
  }, [isEditing]);

  const handleEditClick = () => {
    if (!isProcessing) {
      setEditingMessageId(messageId || null);
      setEditedText(text || '');
    }
  };

  const handleCancelEdit = () => {
    setEditingMessageId(null);
    setEditedText(text || '');
  };

  const handleSaveEdit = () => {
    if (messageId) {
      onSaveEdit(messageId, editedText);
    }
    setEditingMessageId(null);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && e.ctrlKey) {
      e.preventDefault();
      handleSaveEdit();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      handleCancelEdit();
    }
  };

  const handleInput = (e: React.FormEvent<HTMLTextAreaElement>) => {
    const target = e.target as HTMLTextAreaElement;
    target.style.height = 'auto';
    target.style.height = target.scrollHeight + 'px';
  };

  return (
    <ChatBubble
      messageId={messageId}
      containerClassName="user-message"
      bubbleClassName="user-message-bubble-content"
      messageInfo={
        React.isValidElement(messageInfo) && messageInfo.type === MessageInfo // Add type check
          ? React.cloneElement(messageInfo, { onEditClick: handleEditClick } as Partial<MessageInfoProps>) // Cast to Partial<MessageInfoProps>
          : messageInfo
      }
      heighten={false}
    >
      {isEditing ? (
        <textarea
          ref={textareaRef}
          style={{
            width: '100%',
            padding: '8px',
            border: '1px solid #ccc',
            borderRadius: '6px',
            resize: 'vertical',
            boxSizing: 'border-box',
          }}
          value={editedText}
          onChange={(e) => setEditedText(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleCancelEdit}
          onInput={handleInput}
          rows={Math.max(3, editedText.split('\n').length)}
        />
      ) : (
        text
      )}
      <FileAttachmentList attachments={attachments} messageId={messageId} sessionId={sessionId} />
    </ChatBubble>
  );
};

export default UserTextMessage;
