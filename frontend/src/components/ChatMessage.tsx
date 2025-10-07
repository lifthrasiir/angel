import React from 'react';
import type { ChatMessage, EnvChanged, PossibleNextMessage } from '../types/chat';
import { splitOnceByNewline } from '../utils/stringUtils';
import FunctionCallMessage from './FunctionCallMessage';
import FunctionResponseMessage from './FunctionResponseMessage';
import ModelTextMessage from './ModelTextMessage';
import SystemMessage from './SystemMessage';
import UserTextMessage from './UserTextMessage';
import MessageInfo from './MessageInfo';
import CompressionMessage from './CompressionMessage';
import EnvChangedMessage from './EnvChangedMessage';

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
  processingStartTime?: number | null;
  onSaveEdit?: (messageId: string, editedText: string) => void;
  onBranchSelect?: (newBranchId: string) => void;
  allMessages?: ChatMessage[];
  possibleFirstIds?: PossibleNextMessage[];
}

const ChatMessage: React.FC<ChatMessageProps> = React.memo(
  ({
    message,
    maxTokens,
    isLastModelMessage,
    processingStartTime,
    onSaveEdit,
    onBranchSelect,
    allMessages,
    possibleFirstIds,
  }) => {
    const { type, attachments, cumulTokenCount, branchId, parentMessageId, chosenNextId, model } = message;
    const { text, functionCall, functionResponse } = message.parts?.[0] || {};

    // Only show dropdown on messages that can actually switch branches
    let dropdownPossibleNextIds: PossibleNextMessage[] | undefined;

    if (parentMessageId && allMessages) {
      // Case 1: This is a child message - show parent's possibleNextIds if parent has multiple children
      const parentMessage = allMessages.find((msg) => msg.id === parentMessageId);
      if (parentMessage && parentMessage.possibleNextIds && parentMessage.possibleNextIds.length > 1) {
        dropdownPossibleNextIds = parentMessage.possibleNextIds;
      }
    } else if (!parentMessageId && possibleFirstIds && possibleFirstIds.length > 1) {
      // Case 2: This is a virtual root message (no parent) - use server-provided first messages
      const filteredMessages = possibleFirstIds.filter((msg) => msg.messageId !== message.id);
      if (filteredMessages.length > 0) {
        dropdownPossibleNextIds = filteredMessages;
      }
    }
    // Note: Parent messages (that have children) don't get dropdowns anymore

    const messageInfoComponent = (
      <MessageInfo
        cumulTokenCount={cumulTokenCount}
        branchId={branchId}
        parentMessageId={parentMessageId}
        chosenNextId={chosenNextId}
        possibleNextIds={dropdownPossibleNextIds}
        model={model}
        maxTokens={maxTokens}
        onBranchSelect={onBranchSelect}
        sessionId={message.sessionId}
        isVirtualRoot={!parentMessageId && possibleFirstIds && possibleFirstIds.length > 1}
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
          onSaveEdit={onSaveEdit!}
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
          sessionId={message.sessionId}
          messageId={message.id}
          attachments={attachments}
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
