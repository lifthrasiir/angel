import React from 'react';
import type { PossibleNextMessage } from '../types/chat';
import { useAtom } from 'jotai';
import { processingStartTimeAtom } from '../atoms/chatAtoms';
import BranchDropdown from './BranchDropdown';

export interface MessageInfoProps {
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
  onEditClick?: () => void;
  onBranchSelect?: (newBranchId: string) => void;
  sessionId?: string;
  isVirtualRoot?: boolean;
  currentMessageText?: string; // Current message text for diff comparison
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
  onBranchSelect,
  isVirtualRoot = false,
  currentMessageText,
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
    onEditClick ||
    onBranchSelect;

  if (!hasInfo) {
    return null;
  }

  return (
    <div className="message-info" style={{ display: 'flex', alignItems: 'center', gap: '10px', flexWrap: 'wrap' }}>
      <span>
        {model}{' '}
        {cumulTokenCount && maxTokens
          ? `${cumulTokenCount}/${maxTokens}T`
          : cumulTokenCount
            ? `${cumulTokenCount}T`
            : ''}
      </span>
      {onEditClick && (
        <button
          onClick={onEditClick}
          disabled={isProcessing}
          style={{
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
      {onBranchSelect &&
        possibleNextIds &&
        (isVirtualRoot ? possibleNextIds.length > 0 : possibleNextIds.length > 1) && (
          <BranchDropdown
            possibleNextIds={possibleNextIds}
            chosenNextId={chosenNextId}
            currentMessageText={currentMessageText}
            onBranchSelect={onBranchSelect}
            disabled={isProcessing}
          />
        )}
    </div>
  );
};

export default MessageInfo;
