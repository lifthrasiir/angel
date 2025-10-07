import React, { useState, useRef, useEffect } from 'react';
import type { PossibleNextMessage } from '../types/chat';

interface BranchDropdownProps {
  possibleNextIds: PossibleNextMessage[];
  chosenNextId?: string;
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
  possibleNextIds,
  chosenNextId,
  currentMessageText,
  onBranchSelect,
  disabled = false,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Filter to only show branches that are different from the current chosen one
  const availableBranches = possibleNextIds.filter((branch) => branch.messageId !== chosenNextId);

  // Don't render if there are no alternative branches to switch to
  if (availableBranches.length === 0) {
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

  return (
    <div
      ref={dropdownRef}
      style={{
        position: 'relative',
        display: 'inline-block',
        marginLeft: '5px',
      }}
    >
      {/* Dropdown trigger button */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        disabled={disabled}
        style={{
          padding: '4px 8px',
          border: '1px solid #ccc',
          borderRadius: '4px',
          background: disabled ? '#f0f0f0' : '#e0e0e0',
          cursor: disabled ? 'not-allowed' : 'pointer',
          fontSize: '0.8em',
          display: 'flex',
          alignItems: 'center',
          gap: '4px',
        }}
        title="Switch branch"
      >
        <span>⎇</span>
        <span>{availableBranches.length}</span>
      </button>

      {/* Dropdown menu */}
      {isOpen && !disabled && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            right: '0',
            backgroundColor: 'white',
            border: '1px solid #ccc',
            borderRadius: '4px',
            boxShadow: '0 2px 8px rgba(0, 0, 0, 0.1)',
            zIndex: 1000,
            minWidth: '250px',
            maxHeight: '300px',
            overflowY: 'auto',
          }}
        >
          <div
            style={{
              padding: '8px',
              borderBottom: '1px solid #eee',
              fontSize: '0.8em',
              color: '#666',
              fontWeight: 'bold',
            }}
          >
            Switch to branch:
          </div>
          {availableBranches.map((branch) => (
            <button
              key={branch.messageId}
              onClick={() => handleBranchSelect(branch)}
              style={{
                width: '100%',
                padding: '10px 12px',
                border: 'none',
                backgroundColor: 'transparent',
                textAlign: 'left',
                cursor: 'pointer',
                fontSize: '0.85em',
                borderBottom: '1px solid #f0f0f0',
                display: 'flex',
                flexDirection: 'column',
                alignItems: 'flex-start',
                gap: '4px',
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.backgroundColor = '#f5f5f5';
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.backgroundColor = 'transparent';
              }}
            >
              <div
                style={{
                  width: '100%',
                  fontSize: '0.9em',
                  lineHeight: '1.3',
                  color: '#333',
                }}
                title={branch.userText || 'No text'}
              >
                {branch.userText ? formatTextOutput(branch.userText, currentMessageText) : 'No text'}
              </div>
              {branch.timestamp && (
                <div
                  style={{
                    fontSize: '0.75em',
                    color: '#666',
                    alignSelf: 'flex-end',
                  }}
                >
                  {formatRelativeTime(branch.timestamp)}
                </div>
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

export default BranchDropdown;
