import React from 'react';
import type { PossibleNextMessage } from '../types/chat';
import { useAtom } from 'jotai';
import { processingStartTimeAtom } from '../atoms/chatAtoms';

export interface MessageInfoProps {
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
  onEditClick?: () => void;
}

const MessageInfo: React.FC<MessageInfoProps> = ({
  cumulTokenCount,
  branchId,
  parentMessageId,
  chosenNextId,
  possibleNextIds,
  model,
  maxTokens,
  onEditClick,
}) => {
  const [processingStartTime] = useAtom(processingStartTimeAtom);
  const isProcessing = processingStartTime !== null;

  const hasInfo =
    cumulTokenCount !== undefined ||
    branchId ||
    parentMessageId ||
    chosenNextId ||
    (possibleNextIds && possibleNextIds.length > 0) ||
    model ||
    maxTokens ||
    onEditClick;

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
            <React.Fragment key={item.messageId}>
              {item.messageId} ({item.branchId})
            </React.Fragment>
          ))}
        </>
      )}
      {onEditClick && (
        <button
          onClick={onEditClick}
          disabled={isProcessing}
          style={{
            marginLeft: '10px',
            padding: '4px 8px',
            border: '1px solid #ccc',
            borderRadius: '4px',
            background: isProcessing ? '#f0f0f0' : '#e0e0e0',
            cursor: isProcessing ? 'not-allowed' : 'pointer',
            fontSize: '0.8em',
          }}
        >
          Edit
        </button>
      )}
    </div>
  );
};

export default MessageInfo;
