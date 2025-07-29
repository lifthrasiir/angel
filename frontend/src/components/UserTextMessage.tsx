import React from 'react';
import FileAttachmentPreview from './FileAttachmentPreview';

interface FileAttachment {
  fileName: string;
  mimeType: string;
  data: string; // Base64 encoded file content
}

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[]; // New prop
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({ text, attachments }) => {
  return (
    <div className="chat-message-container user-message">
      <div className="chat-bubble">
        {text}
        {attachments && attachments.length > 0 && (
          <div style={{ marginTop: '10px', borderTop: '1px solid #eee', paddingTop: '5px' }}>
            <strong>Attached Files:</strong>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '5px', marginTop: '5px' }}>
              {attachments.map((file, index) => (
                <FileAttachmentPreview key={index} file={file} />
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default UserTextMessage;
