import React, { useState } from 'react';
import { FaRedo } from 'react-icons/fa';

interface RetryErrorButtonProps {
  messageId: string;
  onRetryError: (messageId: string) => void;
  isDisabled?: boolean;
}

const RetryErrorButton: React.FC<RetryErrorButtonProps> = ({ messageId, onRetryError, isDisabled = false }) => {
  const [isHovered, setIsHovered] = useState(false);
  const [isLoading, setIsLoading] = useState(false);

  const handleClick = async () => {
    if (isDisabled || isLoading) return;

    setIsLoading(true);
    try {
      await onRetryError(messageId);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div
      className="retry-error-button-container"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{
        opacity: isHovered ? 1 : 0.8,
        transition: 'all 0.3s ease-in-out',
        transform: isHovered ? 'translateY(-2px)' : 'translateY(0)',
        alignSelf: 'flex-start',
        marginTop: '4px',
        flexShrink: 0,
      }}
    >
      <button
        className="retry-error-button"
        onClick={handleClick}
        disabled={isDisabled || isLoading}
        title={
          isLoading
            ? 'Retrying from error...'
            : isDisabled
              ? 'Cannot retry this error'
              : 'Click to retry from this error message'
        }
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '4px',
          padding: '6px 12px',
          background: 'linear-gradient(135deg, #ff6b6b, #dc3545)',
          color: 'white',
          border: 'none',
          borderRadius: '20px',
          cursor: isDisabled || isLoading ? 'not-allowed' : 'pointer',
          fontSize: '11px',
          fontWeight: '600',
          fontFamily: 'inherit',
          boxShadow: isHovered
            ? '0 4px 12px rgba(220, 53, 69, 0.4), 0 2px 4px rgba(0, 0, 0, 0.2)'
            : '0 2px 6px rgba(220, 53, 69, 0.3), 0 1px 2px rgba(0, 0, 0, 0.1)',
          transition: 'all 0.2s ease-in-out',
          transform: isHovered ? 'scale(1.05)' : 'scale(1)',
          opacity: isDisabled ? 0.6 : 1,
          minHeight: '28px',
          whiteSpace: 'nowrap',
        }}
      >
        {isLoading ? (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '4px',
            }}
          >
            <div
              style={{
                width: '10px',
                height: '10px',
                border: '2px solid #ffffff',
                borderTop: '2px solid transparent',
                borderRadius: '50%',
                animation: 'spin 1s linear infinite',
              }}
            />
            <span>Retrying...</span>
          </div>
        ) : (
          <>
            <FaRedo size={10} style={{ animation: isHovered ? 'spin 2s ease-in-out infinite' : 'none' }} />
            <span style={{ fontWeight: '700' }}>Retry</span>
          </>
        )}
      </button>

      <style>{`
        @keyframes spin {
          0% { transform: rotate(0deg); }
          100% { transform: rotate(360deg); }
        }

        .retry-error-button:hover:not(:disabled) {
          background: linear-gradient(135deg, #ff5252, #c82333) !important;
          box-shadow: 0 6px 16px rgba(220, 53, 69, 0.5), 0 3px 6px rgba(0, 0, 0, 0.3) !important;
        }

        .retry-error-button:active:not(:disabled) {
          transform: scale(0.98) !important;
          box-shadow: 0 2px 4px rgba(220, 53, 69, 0.4), 0 1px 2px rgba(0, 0, 0, 0.2) !important;
        }

        .retry-error-button:disabled {
          cursor: not-allowed !important;
          opacity: 0.6 !important;
        }

        .retry-error-button-container:hover {
          opacity: 1 !important;
        }
      `}</style>
    </div>
  );
};

export default RetryErrorButton;
