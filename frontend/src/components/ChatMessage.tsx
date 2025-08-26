import React from 'react';
import type { ChatMessage, EnvChanged } from '../types/chat'; // EnvChanged 임포트 추가
import { splitOnceByNewline } from '../utils/stringUtils';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import ModelTextMessage from './ModelTextMessage';
import SystemMessage from './SystemMessage';
import UserTextMessage from './UserTextMessage';
import MessageInfo from './MessageInfo';
import CompressionMessage from './CompressionMessage';
import EnvChangedMessage from './EnvChangedMessage'; // EnvChangedMessage 임포트 추가

interface ChatMessageProps {
  message: ChatMessage;
  maxTokens?: number;
  isLastModelMessage?: boolean;
  processingStartTime?: number | null;
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(
  ({ message, maxTokens, isLastModelMessage, processingStartTime }) => {
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
        maxTokens={maxTokens}
      />
    );

    if (type === 'function_response') {
      if (functionResponse)
        return (
          <FunctionResponseMessage
            functionResponse={functionResponse}
            messageInfo={messageInfoComponent}
            messageId={message.id}
          />
        );
    } else if (type === 'user') {
      return (
        <UserTextMessage
          text={text}
          attachments={attachments}
          messageInfo={messageInfoComponent}
          messageId={message.id}
          sessionId={message.sessionId}
        />
      );
    } else if (type === 'thought') {
      const [subject, description] = splitOnceByNewline(text || '');
      const thoughtText = `**Thought: ${subject}**\n${description || ''}`;
      return (
        <ModelTextMessage
          text={thoughtText}
          className="agent-thought"
          messageInfo={messageInfoComponent}
          messageId={message.id}
        />
      );
    } else if (type === 'function_call') {
      if (functionCall)
        return (
          <FunctionCallMessage functionCall={functionCall} messageInfo={messageInfoComponent} messageId={message.id} />
        );
    } else if (type === 'system') {
      return <SystemMessage text={text} messageInfo={messageInfoComponent} messageId={message.id} />;
    } else if (type === 'system_prompt') {
      return <SystemMessage text={text} messageInfo={messageInfoComponent} messageId={message.id} />;
    } else if (type === 'model_error') {
      return (
        <ModelTextMessage
          text={text}
          className="agent-error-message"
          messageInfo={messageInfoComponent}
          messageId={message.id}
        />
      );
    } else if (type === 'compression') {
      return (
        <CompressionMessage
          message={message} // Pass the entire message object
          messageInfo={messageInfoComponent}
        />
      );
    } else if (type === 'env_changed') {
      try {
        const envChangedData: EnvChanged = JSON.parse(text || '{}');
        return <EnvChangedMessage envChanged={envChangedData} messageId={message.id} />;
      } catch (e) {
        console.error('Failed to parse env_changed message:', e);
        return (
          <SystemMessage
            text={`Error displaying environment change: ${text}`}
            messageInfo={messageInfoComponent}
            messageId={message.id}
          />
        );
      }
    } else if (type === 'model') {
      return (
        <ModelTextMessage
          text={text}
          className="agent-message"
          messageInfo={messageInfoComponent}
          isLastModelMessage={isLastModelMessage}
          processingStartTime={processingStartTime}
          messageId={message.id} // Add messageId here
        />
      );
    }

    // Fallback for unknown types or if type is not explicitly set
    return (
      <div id={message.id} className="chat-message-container agent-message">
        <div className="chat-bubble">
          {text} {/* Render raw text as a fallback */}
        </div>
        {messageInfoComponent} {/* Render MessageInfo outside chat-bubble for fallback */}
      </div>
    );
  },
);

export default ChatMessage;
