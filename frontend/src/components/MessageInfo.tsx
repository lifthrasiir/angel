import React from 'react';
import type { PossibleNextMessage } from '../types/chat';

interface MessageInfoProps {
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
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
