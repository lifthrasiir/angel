import React from 'react';
import { FileAttachment } from '../types/chat';
import FileAttachmentPreview from './FileAttachmentPreview';

interface FileAttachmentListProps {
  attachments?: FileAttachment[];
  messageId?: string;
  sessionId?: string;
}

const FileAttachmentList: React.FC<FileAttachmentListProps> = ({ attachments, messageId, sessionId }) => {
  if (!attachments || attachments.length === 0) {
    return null;
  }

  return (
    <div className="user-message-attachments-container">
      <div className="user-message-attachments-list">
        {attachments.map((file, index) => (
          <FileAttachmentPreview
            key={index}
            file={file}
            messageId={messageId}
            sessionId={sessionId}
            blobIndex={index}
          />
        ))}
      </div>
    </div>
  );
};

export default FileAttachmentList;
