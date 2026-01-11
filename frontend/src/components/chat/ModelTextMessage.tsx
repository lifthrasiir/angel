import React, { useRef } from 'react';
import MarkdownRenderer from './MarkdownRenderer';
import { ProcessingIndicator } from './ProcessingIndicator';
import ChatBubble, { type ChatBubbleRef } from './ChatBubble';
import { FileAttachment, ChatMessage } from '../../types/chat';
import FileAttachmentList from '../FileAttachmentList';
import MessageInfo, { type MessageInfoProps } from './MessageInfo';
import { isImageOnlyMessage } from '../../utils/messageUtils';
import CopyButton from './CopyButton';
import { useAtom, useSetAtom } from 'jotai';
import { editingMessageIdAtom, editingSourceAtom } from '../../atoms/uiAtoms';
import { useProcessingState } from '../../hooks/useProcessingState';

interface ModelTextMessageProps {
  text?: string;
  className?: string;
  messageInfo?: React.ReactNode;
  isLastModelMessage?: boolean;
  messageId: string;
  attachments?: FileAttachment[];
  sessionId?: string;
  sideContents?: React.ReactNode;
  message?: ChatMessage;
  isMobile?: boolean;
  onSaveUpdate?: (messageId: string, editedText: string) => void;
  onContinueClick?: (messageId: string) => void;
}

const ModelTextMessage: React.FC<ModelTextMessageProps> = ({
  text,
  className,
  messageInfo,
  isLastModelMessage,
  messageId,
  attachments,
  sessionId,
  sideContents,
  message,
  isMobile = false,
  onSaveUpdate,
  onContinueClick,
}) => {
  const { isProcessing } = useProcessingState();
  const [editingMessageId, setEditingMessageId] = useAtom(editingMessageIdAtom);
  const setEditingSource = useSetAtom(editingSourceAtom);
  const chatBubbleRef = useRef<ChatBubbleRef>(null);

  const isEditing = messageId === editingMessageId;
  const imageOnly = isImageOnlyMessage(text, attachments);
  const isUpdated = message?.aux?.beforeUpdate !== undefined;

  const handleUpdateClick = () => {
    if (!isProcessing && !imageOnly) {
      setEditingMessageId(messageId || null);
      setEditingSource('update');
    }
  };

  const handleUpdateSave = (newText: string) => {
    if (messageId && onSaveUpdate) {
      onSaveUpdate(messageId, newText);
    }
    setEditingMessageId(null);
    setEditingSource(null);
  };

  const handleContinue = () => {
    if (onContinueClick && messageId) {
      onContinueClick(messageId);
    }
  };

  const handleUpdateCancel = () => {
    setEditingMessageId(null);
    setEditingSource(null);
  };

  // If there's already sideContents (e.g., RetryErrorButton), use it; otherwise add CopyButton
  const finalSideContents = sideContents || (text && <CopyButton text={text} />);

  return (
    <ChatBubble
      ref={chatBubbleRef}
      messageId={messageId}
      containerClassName={className}
      messageInfo={
        React.isValidElement(messageInfo) && messageInfo.type === MessageInfo
          ? React.cloneElement(messageInfo, {
              onUpdateClick: onSaveUpdate && !imageOnly ? handleUpdateClick : undefined,
              onContinueClick: isUpdated ? handleContinue : undefined,
              isEditing: isEditing && !imageOnly,
              onEditSave: () => {
                if (chatBubbleRef.current) {
                  chatBubbleRef.current.saveEdit();
                }
              },
              onEditCancel: handleUpdateCancel,
              message: message,
              isMobile: isMobile,
              branchDropdownAlign: 'left',
            } as Partial<MessageInfoProps>)
          : messageInfo
      }
      sideContents={finalSideContents}
      editText={!imageOnly ? text : ''}
      isEditing={isEditing && !imageOnly}
      onEditSave={handleUpdateSave}
      onEditCancel={handleUpdateCancel}
    >
      <FileAttachmentList attachments={attachments} sessionId={sessionId} isImageOnlyMessage={imageOnly} />
      {!imageOnly && <MarkdownRenderer content={text || ''} />}
      {isLastModelMessage && <ProcessingIndicator isLastThoughtGroup={false} isLastModelMessage={true} />}
    </ChatBubble>
  );
};

export default ModelTextMessage;
