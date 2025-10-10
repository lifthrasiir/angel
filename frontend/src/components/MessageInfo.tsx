import React from 'react';
import type { PossibleNextMessage } from '../types/chat';
import { useAtom } from 'jotai';
import { processingStartTimeAtom } from '../atoms/chatAtoms';
import { FaEdit, FaRedo } from 'react-icons/fa';
import BranchDropdown from './BranchDropdown';

export interface MessageInfoProps {
  cumulTokenCount?: number | null;
  possibleBranches?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
  onEditClick?: () => void;
  onRetryClick?: () => void;
  onBranchSelect?: (newBranchId: string) => void;
  sessionId?: string;
  currentMessageText?: string; // Current message text for diff comparison
}

const MessageInfo: React.FC<MessageInfoProps> = React.memo(
  ({
    cumulTokenCount,
    possibleBranches,
    model,
    maxTokens,
    onEditClick,
    onRetryClick,
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
      onRetryClick ||
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
          <button onClick={onEditClick} disabled={isProcessing} title="Edit message">
            <FaEdit size={16} />
          </button>
        )}
        {onRetryClick && (
          <button onClick={onRetryClick} disabled={isProcessing} title="Retry message">
            <FaRedo size={16} />
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
  },
);

MessageInfo.displayName = 'MessageInfo';

export default MessageInfo;
