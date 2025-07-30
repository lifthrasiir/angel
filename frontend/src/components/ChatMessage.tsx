import React from 'react';
import 'katex/dist/katex.min.css'; // Add this import for KaTeX CSS

// Import new message components
import UserTextMessage from './UserTextMessage';
import ModelTextMessage from './ModelTextMessage';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import SystemMessage from './SystemMessage';
import type { ChatMessage } from '../hooks/useChatSession';

const ChatMessage: React.FC<{ message: ChatMessage }> = React.memo(({ message }) => {
  const { role, type, attachments } = message;
  const { text, functionCall, functionResponse } = message.parts[0] || {};

  if (type === 'function_response') {
    return <FunctionResponseMessage functionResponse={functionResponse} isUserRole={role === 'user'} />;
  } else if (type === 'user') {
    return <UserTextMessage text={text} attachments={attachments} />;
  } else if (type === 'thought') {
    // Thought messages are handled by ThoughtGroup, which passes them to ChatMessage.
    // We need to render them as a ModelTextMessage with special styling.
    const [subject, description] = (text || '').split('\n', 2);
    const thoughtText = `**Thought: ${subject}**\n${description || ''}`;
    return <ModelTextMessage text={thoughtText} className="agent-thought" />;
  } else if (type === 'function_call') {
    return <FunctionCallMessage functionCall={functionCall} />;
  } else if (type === 'system') {
    return <SystemMessage text={text} />;
  } else if (type === 'model') {
    return <ModelTextMessage text={text} />;
  }

  // Fallback for unknown types or if type is not explicitly set
  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble">
        {text} {/* Render raw text as a fallback */}
      </div>
    </div>
  );
});

export default ChatMessage;
