import React, { useState, useRef, useEffect } from 'react';
import type { PossibleNextMessage } from '../types/chat';
import { FaCodeBranch } from 'react-icons/fa';

interface BranchDropdownProps {
  possibleBranches: PossibleNextMessage[];
  currentMessageText?: string; // Current message text for diff comparison
  onBranchSelect: (newBranchId: string) => void;
  disabled?: boolean;
}

// Helper function to format relative time
const formatRelativeTime = (timestamp: number): string => {
  const now = Date.now();
  const diff = now - timestamp * 1000; // Convert Unix timestamp to milliseconds
  const minutes = Math.floor(diff / (1000 * 60));
  const hours = Math.floor(diff / (1000 * 60 * 60));
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));

  if (minutes < 1) return 'just now';
  if (minutes < 60) return `${minutes}m ago`;
  if (hours < 24) return `${hours}h ago`;
  return `${days}d ago`;
};

// Helper function to find common prefix and suffix between two strings
const findCommonPrefixAndSuffix = (
  str1: string,
  str2: string,
): { prefix: string; suffix: string; diff1: string; diff2: string } => {
  let commonPrefixLength = 0;
  let commonSuffixLength = 0;

  // Find common prefix
  const minLen = Math.min(str1.length, str2.length);
  while (commonPrefixLength < minLen && str1[commonPrefixLength] === str2[commonPrefixLength]) {
    commonPrefixLength++;
  }

  // Find common suffix
  while (
    commonSuffixLength < minLen - commonPrefixLength &&
    str1[str1.length - 1 - commonSuffixLength] === str2[str2.length - 1 - commonSuffixLength]
  ) {
    commonSuffixLength++;
  }

  const prefix = str1.substring(0, commonPrefixLength);
  const suffix = commonSuffixLength > 0 ? str1.substring(str1.length - commonSuffixLength) : '';
  const diff1 = str1.substring(commonPrefixLength, str1.length - commonSuffixLength);
  const diff2 = str2.substring(commonPrefixLength, str2.length - commonSuffixLength);

  return { prefix, suffix, diff1, diff2 };
};

// Helper function to truncate text with ellipsis if it exceeds limit
const truncateWithEllipsis = (text: string, limit: number): string => {
  if (text.length <= limit) {
    return text;
  }
  return '…' + text.substring(text.length - limit);
};

// Helper function to create diff summary with length limitation
const createDiffSummary = (diff: string, maxLength: number): string => {
  if (diff.length <= maxLength) {
    return diff;
  }

  // Take some from beginning and end with ellipsis in middle
  const startLength = Math.floor(maxLength / 2);
  const endLength = maxLength - startLength - 1; // -1 for ellipsis

  return diff.substring(0, startLength) + '…' + diff.substring(diff.length - endLength);
};

// Helper function to format text output according to specifications
const formatTextOutput = (newText: string, currentText?: string): JSX.Element => {
  const M = 100; // Maximum total length
  const N = 10; // Maximum prefix/suffix length before truncation

  // Case 1: If strings are identical, return as-is
  if (currentText && newText === currentText) {
    return <>{newText.length <= M ? newText : newText.substring(0, M - 1) + '…'}</>;
  }

  // Case 2: If no current text for comparison
  if (!currentText) {
    return <strong>{newText.length <= M ? newText : newText.substring(0, M - 1) + '…'}</strong>;
  }

  // Find common prefix and suffix
  const { prefix, suffix, diff1 } = findCommonPrefixAndSuffix(newText, currentText);

  // Case 2: Total length is within limit
  if (newText.length <= M) {
    if (diff1.length === 0) {
      return (
        <>
          {prefix}
          <span>▯</span>
          {suffix}
        </>
      );
    } else {
      return (
        <>
          {prefix}
          <strong>{diff1}</strong>
          {suffix}
        </>
      );
    }
  }

  // Case 3: Truncate long prefix/suffix if they exceed N
  const truncatedPrefix = truncateWithEllipsis(prefix, N);
  const truncatedSuffix = suffix.length > N ? suffix.substring(0, N) + '…' : suffix;

  // Calculate remaining space for diff
  const M_prime = M - truncatedPrefix.length - truncatedSuffix.length;

  // Case 4: Summarize diff to fit within remaining space
  const diffSummary = createDiffSummary(diff1, M_prime);

  // Case 5: Handle empty diff case
  if (diffSummary.length === 0) {
    return (
      <>
        {truncatedPrefix}
        <span>▯</span>
        {truncatedSuffix}
      </>
    );
  }

  return (
    <>
      {truncatedPrefix}
      <strong>{diffSummary}</strong>
      {truncatedSuffix}
    </>
  );
};

const BranchDropdown: React.FC<BranchDropdownProps> = ({
  possibleBranches,
  currentMessageText,
  onBranchSelect,
  disabled = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [dropDirection, setDropDirection] = useState<'down' | 'up'>('down');
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Don't render if there are no alternative branches to switch to
  if (possibleBranches.length === 0) {
    return null;
  }

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  const handleBranchSelect = (branch: PossibleNextMessage) => {
    onBranchSelect(branch.branchId);
    setIsOpen(false);
  };

  // Calculate dropdown direction based on viewport
  const calculateDropDirection = () => {
    if (!dropdownRef.current) return;

    const rect = dropdownRef.current.getBoundingClientRect();
    const centerY = window.innerHeight / 2;

    if (rect.bottom < centerY) {
      setDropDirection('down');
    } else {
      setDropDirection('up');
    }
  };

  return (
    <div
      ref={dropdownRef}
      style={{
        position: 'relative',
        display: 'inline-block',
      }}
    >
      {/* Dropdown trigger button */}
      <button
        onClick={() => {
          if (!isOpen) {
            calculateDropDirection();
          }
          setIsOpen(!isOpen);
        }}
        disabled={disabled}
        className="branch-dropdown-trigger"
        title={`Switch branch (${possibleBranches.length} available)`}
      >
        <FaCodeBranch size={16} />
        <span className="branch-count">{possibleBranches.length}</span>
      </button>

      {/* Dropdown menu */}
      {isOpen && !disabled && (
        <div
          className="branch-dropdown-menu"
          style={{
            top: dropDirection === 'up' ? 'auto' : '100%',
            bottom: dropDirection === 'up' ? '100%' : 'auto',
          }}
        >
          <div className="branch-dropdown-header">Switch to branch:</div>
          {possibleBranches
            .slice()
            .sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0))
            .map((branch) => (
              <button
                key={branch.messageId}
                onClick={() => handleBranchSelect(branch)}
                className="branch-dropdown-item"
              >
                <div className="branch-item-content">
                  <div className="branch-text" title={branch.userText || 'No text'}>
                    {branch.userText ? formatTextOutput(branch.userText, currentMessageText) : 'No text'}
                  </div>
                  {branch.timestamp && <div className="branch-time">{formatRelativeTime(branch.timestamp)}</div>}
                </div>
              </button>
            ))}
        </div>
      )}
    </div>
  );
};

export default BranchDropdown;
