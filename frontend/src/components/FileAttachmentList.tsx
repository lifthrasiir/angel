import React from 'react';
import { FileAttachment } from '../types/chat';
import FileAttachmentPreview from './FileAttachmentPreview';

interface FileAttachmentListProps {
  attachments?: FileAttachment[];
  onRemove?: (file: File) => void;
  isImageOnlyMessage?: boolean;
}

const FileAttachmentList: React.FC<FileAttachmentListProps> = ({
  attachments,
  onRemove,
  isImageOnlyMessage = false,
}) => {
  if (!attachments || attachments.length === 0) {
    return null;
  }

  const containerClassName = isImageOnlyMessage
    ? 'image-only-message-attachments-container'
    : 'user-message-attachments-container';

  const listClassName = isImageOnlyMessage ? 'image-only-message-attachments-list' : 'user-message-attachments-list';

  return (
    <div className={containerClassName}>
      <div className={listClassName}>
        {attachments.map((file, index) => (
          <FileAttachmentPreview key={index} file={file} onRemove={onRemove} isImageOnlyMessage={isImageOnlyMessage} />
        ))}
      </div>
    </div>
  );
};

export default FileAttachmentList;
