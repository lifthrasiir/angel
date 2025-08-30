import React from 'react';
import type { ChatMessage } from '../types/chat';
import { splitOnceByNewline } from '../utils/stringUtils';
import ChatBubble from './ChatBubble';

interface CompressionMessageProps {
  message: ChatMessage;
  messageInfo: React.ReactNode;
}

const CompressionMessage: React.FC<CompressionMessageProps> = ({ message, messageInfo }) => {
  const fullText = message.parts?.[0]?.text || '';
  const [firstLine, ...restLines] = splitOnceByNewline(fullText);
  const remainingText = restLines.join('\n');

  return (
    <ChatBubble
      messageId={message.id}
      containerClassName="system-message compression-message"
      messageInfo={messageInfo}
    >
      <p>
        Compression Snapshot before{' '}
        <a href={`#${firstLine}`} className="message-id-link">
          {firstLine}
        </a>
      </p>
      {remainingText && <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{remainingText}</pre>}
    </ChatBubble>
  );
};

export default CompressionMessage;
