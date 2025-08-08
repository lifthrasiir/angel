import React from 'react';
import type { ChatMessage } from '../types/chat';
import { splitOnceByNewline } from '../utils/stringUtils';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import ModelTextMessage from './ModelTextMessage';
import SystemMessage from './SystemMessage';
// Import new message components
import UserTextMessage from './UserTextMessage';
import MessageInfo from './MessageInfo'; // Import MessageInfo

const ChatMessage: React.FC<{ message: ChatMessage }> = React.memo(({ message }) => {
  const { type, attachments, cumulTokenCount, branchId, parentMessageId, chosenNextId, possibleNextIds, model } =
    message;
  const { text, functionCall, functionResponse } = message.parts?.[0] || {};

  const messageInfoComponent = (
    <MessageInfo
      cumulTokenCount={cumulTokenCount}
      branchId={branchId}
      parentMessageId={parentMessageId}
      chosenNextId={chosenNextId}
      possibleNextIds={possibleNextIds}
      model={model}
    />
  );

  if (type === 'function_response' && functionResponse) {
    return <FunctionResponseMessage functionResponse={functionResponse} messageInfo={messageInfoComponent} />;
  } else if (type === 'user') {
    return <UserTextMessage text={text} attachments={attachments} messageInfo={messageInfoComponent} />;
  } else if (type === 'thought') {
    const [subject, description] = splitOnceByNewline(text || '');
    const thoughtText = `**Thought: ${subject}**\n${description || ''}`;
    return <ModelTextMessage text={thoughtText} className="agent-thought" messageInfo={messageInfoComponent} />;
  } else if (type === 'function_call' && functionCall) {
    return <FunctionCallMessage functionCall={functionCall} messageInfo={messageInfoComponent} />;
  } else if (type === 'system') {
    return <SystemMessage text={text} messageInfo={messageInfoComponent} />;
  } else if (type === 'model_error') {
    return <ModelTextMessage text={text} className="agent-error-message" messageInfo={messageInfoComponent} />;
  } else if (type === 'model') {
    return <ModelTextMessage text={text} className="agent-message" messageInfo={messageInfoComponent} />;
  }

  // Fallback for unknown types or if type is not explicitly set
  return (
    <div className="chat-message-container agent-message">
      <div className="chat-bubble">
        {text} {/* Render raw text as a fallback */}
      </div>
      {messageInfoComponent} {/* Render MessageInfo outside chat-bubble for fallback */}
    </div>
  );
});

export default ChatMessage;
