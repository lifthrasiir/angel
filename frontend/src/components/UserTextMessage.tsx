import React from 'react';

interface UserTextMessageProps {
  text?: string;
}

const UserTextMessage: React.FC<UserTextMessageProps> = ({ text }) => {
  return (
    <div className="chat-message-container user-message">
      <div className="chat-bubble">
        {text}
      </div>
    </div>
  );
};

export default UserTextMessage;
