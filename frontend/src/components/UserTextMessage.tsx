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
        ref={messageRef}
        className={`chat-bubble user-message-bubble-content ${isExpanded ? 'expanded' : 'collapsed'}`}
      >
        {text}
        {attachments && attachments.length > 0 && (
          <div className="user-message-attachments-container">
            <div className="user-message-attachments-list">
              {attachments.map((file, index) => (
                <FileAttachmentPreview key={index} file={file} />
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
    </div>
  );
};

export default UserTextMessage;
