import React from 'react';
import type { PossibleNextMessage } from '../types/chat';
import { useAtom } from 'jotai';
import { processingStartTimeAtom } from '../atoms/chatAtoms';
import BranchDropdown from './BranchDropdown';

export interface MessageInfoProps {
  cumulTokenCount?: number | null;
  possibleBranches?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
  onEditClick?: () => void;
  onBranchSelect?: (newBranchId: string) => void;
  sessionId?: string;
  currentMessageText?: string; // Current message text for diff comparison
}

const MessageInfo: React.FC<MessageInfoProps> = ({
  cumulTokenCount,
  possibleBranches,
  model,
  maxTokens,
  onEditClick,
  onBranchSelect,
  currentMessageText,
}) => {
  const [processingStartTime] = useAtom(processingStartTimeAtom);
  const isProcessing = processingStartTime !== null;

  const hasInfo =
    cumulTokenCount !== undefined ||
    (possibleBranches && possibleBranches.length > 0) ||
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
      {onBranchSelect && possibleBranches && possibleBranches.length > 0 && (
        <BranchDropdown
          possibleBranches={possibleBranches}
          currentMessageText={currentMessageText}
          onBranchSelect={onBranchSelect}
          disabled={isProcessing}
        />
      )}
    </div>
  );
};

export default MessageInfo;
