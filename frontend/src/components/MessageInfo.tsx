import React from 'react';

interface MessageInfoProps {
  cumulTokenCount?: number | null;
  // Add other message-related info props here in the future
}

const MessageInfo: React.FC<MessageInfoProps> = ({ cumulTokenCount }) => {
  if (cumulTokenCount === undefined || cumulTokenCount === null) {
    return null;
  }

  return (
    <div className="message-info">
      <div className="token-count">Tokens: {cumulTokenCount}</div>
    </div>
  );
};

export default MessageInfo;
