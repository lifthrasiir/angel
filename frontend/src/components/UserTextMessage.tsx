import type React from 'react';
import { useLayoutEffect, useRef, useState } from 'react';
import { FaChevronCircleDown, FaChevronCircleUp } from 'react-icons/fa';
import type { FileAttachment } from '../types/chat';
import { measureContentHeight } from '../utils/measurementUtils';
import FileAttachmentPreview from './FileAttachmentPreview';

interface UserTextMessageProps {
  text?: string;
  attachments?: FileAttachment[];
  messageInfo?: React.ReactNode;
  messageId?: string; // New: message ID from ChatMessage
  sessionId?: string; // New: session ID from ChatMessage
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({ text, attachments, messageInfo, messageId, sessionId }) => {
  console.log('UserTextMessage attachments:', attachments);
  const [isExpanded, setIsExpanded] = useState(false);
  const [showToggle, setShowToggle] = useState(false);
  const messageRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (messageRef.current) {
      const contentHeight = measureContentHeight(
        messageRef,
        false, // showPrettyJson is false for UserTextMessage
        text || '',
        null, // data is not directly used for UserTextMessage
      );
      const collapsedHeight = window.innerHeight * 0.3;
      setShowToggle(contentHeight > collapsedHeight);
    }
  }, [text, attachments]);

  const toggleExpand = () => {
    setIsExpanded(!isExpanded);
  };

  return (
    <div className="chat-message-container user-message">
      <div
        ref={messageRef}
        className={`chat-bubble user-message-bubble-content ${isExpanded ? 'expanded' : 'collapsed'}`}
        style={showToggle && !isExpanded ? { maxHeight: '30vh', overflowY: 'auto' } : {}}
      >
        {text}
        {attachments && attachments.length > 0 && (
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
        )}
        {showToggle && (
          <div className="user-message-toggle-button" onClick={toggleExpand}>
            {isExpanded ? <FaChevronCircleUp /> : <FaChevronCircleDown />}
          </div>
        )}
      </div>
      {messageInfo} {/* Render MessageInfo outside chat-bubble */}
    </div>
  );
};

export default UserTextMessage;
