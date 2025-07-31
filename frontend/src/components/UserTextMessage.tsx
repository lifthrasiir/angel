import React, { useState, useRef, useEffect } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import FileAttachmentPreview from './FileAttachmentPreview';
import { FileAttachment } from '../types/chat';

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[];
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({ text, attachments }) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const [showToggle, setShowToggle] = useState(false);
  const messageRef = useRef<HTMLDivElement>(null);

  const maxHeight = '30vh';

  useEffect(() => {
    if (messageRef.current) {
      setShowToggle(messageRef.current.scrollHeight > messageRef.current.clientHeight);
    }
  }, [text, attachments]);

  const toggleExpand = () => {
    setIsExpanded(!isExpanded);
  };

  return (
    <div className="chat-message-container user-message">
      <div
        className="chat-bubble"
        ref={messageRef}
        style={{
          maxHeight: isExpanded ? 'none' : maxHeight,
          overflowY: isExpanded ? 'visible' : 'auto',
          position: 'relative',
        }}
      >
        {text}
        {attachments && attachments.length > 0 && (
          <div style={{ marginTop: '10px', borderTop: '1px solid #eee', paddingTop: '5px' }}>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '5px', marginTop: '5px' }}>
              {attachments.map((file, index) => (
                <FileAttachmentPreview key={index} file={file} />
              ))}
            </div>
          </div>
        )}
        {showToggle && (
          <div
            style={{
              position: 'sticky',
              bottom: '-10px',
              left: '0',
              width: '100%',
              textAlign: 'center',
              cursor: 'pointer',
              color: 'var(--color-user-verydark)',
              fontSize: '1.2em',
              zIndex: 10,
              backgroundColor: 'var(--color-user-light)',
              paddingTop: '10px',
            }}
            onClick={toggleExpand}
          >
            {isExpanded ? <FaChevronCircleUp /> : <FaChevronCircleDown />}
          </div>
        )}
      </div>
    </div>
  );
};

export default UserTextMessage;
