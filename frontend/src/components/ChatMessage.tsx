import React from 'react';
import type { ChatMessage, EnvChanged } from '../types/chat';
import { splitOnceByNewline } from '../utils/stringUtils';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import ModelTextMessage from './ModelTextMessage';
import SystemMessage from './SystemMessage';
import UserTextMessage from './UserTextMessage';
import MessageInfo from './MessageInfo';
import CompressionMessage from './CompressionMessage';
import EnvChangedMessage from './EnvChangedMessage';
import RetryErrorButton from './RetryErrorButton';
import './ChatMessage.css';

// Helper function to extract text content from a message
const getMessageText = (message: ChatMessage): string => {
  if (message.parts && message.parts.length > 0) {
    // Return the text content of the first part
    return message.parts[0].text || '';
  }
  return '';
};

interface ChatMessageProps {
  message: ChatMessage;
  maxTokens?: number;
  isLastModelMessage?: boolean;
  onSaveEdit?: (messageId: string, editedText: string) => void;
  onRetryClick?: (messageId: string) => void;
  onRetryError?: (messageId: string) => void;
  onBranchSelect?: (newBranchId: string) => void;
  isMobile?: boolean;
  isMostRecentUserMessage?: boolean;
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(
  ({
    message,
    maxTokens,
    isLastModelMessage,
    onSaveEdit,
    onRetryClick,
    onRetryError,
    onBranchSelect,
    isMobile = false,
    isMostRecentUserMessage = false,
  }) => {
    const { type, attachments, cumulTokenCount, model } = message;
    const { text, functionCall, functionResponse } = message.parts?.[0] || {};

    const messageInfoComponent = (
      <MessageInfo
        cumulTokenCount={cumulTokenCount}
        possibleBranches={message.possibleBranches}
        model={model}
        maxTokens={maxTokens}
        onBranchSelect={onBranchSelect}
        sessionId={message.sessionId}
        currentMessageText={getMessageText(message)}
      />
    );

    if (type === 'function_response') {
      if (functionResponse)
        return (
          <FunctionResponseMessage
            functionResponse={functionResponse}
            messageInfo={messageInfoComponent}
            messageId={message.id}
            attachments={attachments}
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
          message={message}
          onSaveEdit={onSaveEdit!}
          onRetryClick={onRetryClick}
          isMobile={isMobile}
          isMostRecentUserMessage={isMostRecentUserMessage}
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
          message={message}
          isMobile={isMobile}
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
          message={message}
          isMobile={isMobile}
          sideContents={
            onRetryError && <RetryErrorButton messageId={message.id} onRetryError={onRetryError} isDisabled={false} />
          }
        />
      );
    } else if (type === 'compression') {
      return (
        <CompressionMessage
          message={message} // Pass the entire message object
          messageInfo={messageInfoComponent}
        />
      );
    } else if (type === 'command') {
      // Render command as horizontal rule
      const commandText = text || '???';
      return (
        <div className="command-message">
          <hr />
          <div className="command-text">/{commandText}</div>
          <hr />
        </div>
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
          sessionId={message.sessionId}
          messageId={message.id}
          attachments={attachments}
          message={message}
          isMobile={isMobile}
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
