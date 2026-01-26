import React from 'react';
import type { PossibleNextMessage, ChatMessage } from '../../types/chat';
import { FaEdit, FaRedo, FaTimes, FaPaperPlane, FaArrowDown, FaSave } from 'react-icons/fa';
import BranchDropdown from './BranchDropdown';
import MessageMenu from './MessageMenu';
import { useProcessingState } from '../../hooks/useProcessingState';
import { useAtomValue } from 'jotai';
import { editingSourceAtom } from '../../atoms/uiAtoms';
import './MessageInfo.css';

export interface MessageInfoProps {
  cumulTokenCount?: number | null;
  possibleBranches?: PossibleNextMessage[];
  model?: string;
  maxTokens?: number;
  onEditClick?: () => void; // Edit with retry (creates new branch)
  onUpdateClick?: () => void; // Update without retry (just save changes)
  onRetryClick?: () => void;
  onContinueClick?: () => void; // Continue button for updated model messages
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
  continueAccessKey?: string; // Access key for continue button
  branchDropdownAlign?: 'left' | 'right'; // Direction of branch dropdown menu
  isDisabled?: boolean; // Whether editing/retry/continue is disabled
}

const MessageInfo: React.FC<MessageInfoProps> = React.memo(
  ({
    cumulTokenCount,
    possibleBranches,
    model,
    maxTokens,
    onEditClick,
    onUpdateClick,
    onRetryClick,
    onContinueClick,
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
    continueAccessKey,
    branchDropdownAlign = 'right',
    isDisabled = false,
  }) => {
    const { isProcessing } = useProcessingState();
    const editingSource = useAtomValue(editingSourceAtom);
    const hasInfo =
      cumulTokenCount !== undefined ||
      (possibleBranches && possibleBranches.length > 0) ||
      model ||
      maxTokens ||
      onEditClick ||
      onUpdateClick ||
      onRetryClick ||
      onContinueClick ||
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
                title={editingSource === 'update' ? 'Save' : 'Send'}
                className="edit-confirm-btn"
                aria-label={editingSource === 'update' ? 'Save update' : 'Send edit'}
              >
                {editingSource === 'update' ? <FaSave size={16} /> : <FaPaperPlane size={16} />}
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
              <button
                onClick={onEditClick}
                disabled={isProcessing || isDisabled}
                title="Edit message"
                accessKey={editAccessKey}
              >
                <FaEdit size={16} />
              </button>
            )}
            {onContinueClick && (
              <button
                onClick={onContinueClick}
                disabled={isProcessing || isDisabled}
                title="Continue after this point"
                accessKey={continueAccessKey}
              >
                <FaArrowDown size={16} />
              </button>
            )}
            {onRetryClick && (
              <button
                onClick={onRetryClick}
                disabled={isProcessing || isDisabled}
                title="Retry message"
                accessKey={retryAccessKey}
              >
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
            align={branchDropdownAlign}
          />
        )}
        {message && sessionId && (message.type === 'model' || message.type === 'user') && (
          <MessageMenu
            message={message}
            sessionId={sessionId}
            isMobile={isMobile}
            onUpdateClick={onUpdateClick}
            isDisabled={isDisabled}
          />
        )}
      </div>
    );
  },
);

MessageInfo.displayName = 'MessageInfo';

export default MessageInfo;
