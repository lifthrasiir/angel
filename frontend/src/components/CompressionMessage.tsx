import React from 'react';
import type { ChatMessage } from '../types/chat';
import { splitOnceByNewline } from '../utils/stringUtils';

interface CompressionMessageProps {
  message: ChatMessage;
  messageInfo: React.ReactNode; // Pass messageInfo as a prop
}

const CompressionMessage: React.FC<CompressionMessageProps> = ({ message, messageInfo }) => {
  const fullText = message.parts?.[0]?.text || '';
  const [firstLine, ...restLines] = splitOnceByNewline(fullText);
  const remainingText = restLines.join('\n');

  return (
    <div id={message.id} className="chat-message-container system-message compression-message">
      <div className="chat-bubble">
        <p>
          Compression Snapshot:{' '}
          <a href={`#${firstLine}`} className="message-id-link">
            {firstLine}
          </a>
        </p>
        {remainingText && <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{remainingText}</pre>}
      </div>
      {messageInfo}
    </div>
  );
};

export default CompressionMessage;
