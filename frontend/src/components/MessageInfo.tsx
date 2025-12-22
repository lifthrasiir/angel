import React from 'react';
import type { PossibleNextMessage, ChatMessage } from '../types/chat';
import { FaEdit, FaRedo, FaTimes, FaPaperPlane } from 'react-icons/fa';
import BranchDropdown from './BranchDropdown';
import MessageMenu from './MessageMenu';
import { useProcessingState } from '../hooks/useProcessingState';
import './MessageInfo.css';

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
  isEditing?: boolean; // Whether the message is currently being edited
  onEditSave?: () => void; // Callback for edit save
  onEditCancel?: () => void; // Callback for edit cancel
  message?: ChatMessage; // Full message object for menu
  isMobile?: boolean;
  editAccessKey?: string; // Access key for edit button
  retryAccessKey?: string; // Access key for retry button
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
    sessionId,
    currentMessageText,
    isEditing = false,
    onEditSave,
    onEditCancel,
    message,
    isMobile = false,
    editAccessKey,
    retryAccessKey,
  }) => {
    const { isProcessing } = useProcessingState();
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
        {isEditing ? (
          <>
            {onEditSave && (
              <button
                onClick={onEditSave}
                disabled={isProcessing}
                title="Save edit"
                className="edit-confirm-btn"
                aria-label="Save edit"
              >
                <FaPaperPlane size={16} />
              </button>
            )}
            {onEditCancel && (
              <button
                onClick={onEditCancel}
                disabled={isProcessing}
                title="Cancel edit"
                className="edit-cancel-btn"
                aria-label="Cancel edit"
              >
                <FaTimes size={16} />
              </button>
            )}
          </>
        ) : (
          <>
            {onEditClick && (
              <button onClick={onEditClick} disabled={isProcessing} title="Edit message" accessKey={editAccessKey}>
                <FaEdit size={16} />
              </button>
            )}
            {onRetryClick && (
              <button onClick={onRetryClick} disabled={isProcessing} title="Retry message" accessKey={retryAccessKey}>
                <FaRedo size={16} />
              </button>
            )}
          </>
        )}
        {onBranchSelect && possibleBranches && possibleBranches.length > 0 && (
          <BranchDropdown
            possibleBranches={possibleBranches}
            currentMessageText={currentMessageText}
            onBranchSelect={onBranchSelect}
            disabled={isProcessing}
          />
        )}
        {message && sessionId && (message.type === 'model' || message.type === 'user') && (
          <MessageMenu message={message} sessionId={sessionId} isMobile={isMobile} />
        )}
      </div>
    );
  },
);

MessageInfo.displayName = 'MessageInfo';

export default MessageInfo;
