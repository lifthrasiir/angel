import React from 'react';
import type { PossibleNextMessage } from '../types/chat';

interface MessageInfoProps {
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string; // New prop for the model that generated the message
  maxTokens?: number; // New prop for the maximum tokens of the model
}

const MessageInfo: React.FC<MessageInfoProps> = ({
  cumulTokenCount,
  branchId,
  parentMessageId,
  chosenNextId,
  possibleNextIds,
  model,
  maxTokens,
}) => {
  const hasInfo =
    cumulTokenCount !== undefined ||
    branchId ||
    parentMessageId ||
    chosenNextId ||
    (possibleNextIds && possibleNextIds.length > 0) ||
    model ||
    maxTokens;

  if (!hasInfo) {
    return null;
  }

  return (
    <div className="message-info">
      {model}{' '}
      {cumulTokenCount && maxTokens ? `${cumulTokenCount}/${maxTokens}T` : cumulTokenCount ? `${cumulTokenCount}T` : ''}
      {possibleNextIds && possibleNextIds.length > 1 && (
        <>
          | Next:{' '}
          {possibleNextIds.map((item) => (
            <>
              {item.messageId} ({item.branchId})
            </>
          ))}
        </>
      )}
    </div>
  );
};

export default MessageInfo;
