import React, { useState } from 'react';
import { FaPlus } from 'react-icons/fa';

interface NewSessionButtonProps {
  showWorkspaces: boolean;
  showTemporarySessionButton: boolean;
  onClick: () => void;
  onDrop: (e: React.DragEvent<HTMLButtonElement>) => void;
  onDragOver: (e: React.DragEvent<HTMLButtonElement>) => void;
  onDragLeave: (e: React.DragEvent<HTMLButtonElement>) => void;
}

export const NewSessionButton: React.FC<NewSessionButtonProps> = ({
  showWorkspaces,
  showTemporarySessionButton,
  onClick,
  onDrop,
  onDragOver,
  onDragLeave,
}) => {
  const [isDragging, setIsDragging] = useState(false);

  return (
    <div style={{ position: 'relative', width: '100%' }}>
      {isDragging && (
        <div
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 123, 255, 0.1)',
            border: '2px solid rgba(0, 123, 255, 0.3)',
            borderRadius: '4px',
            pointerEvents: 'none',
            zIndex: 1,
          }}
        />
      )}
      <button
        onClick={onClick}
        onDrop={onDrop}
        onDragOver={(e) => {
          onDragOver(e);
          setIsDragging(true);
        }}
        onDragLeave={(e) => {
          onDragLeave(e);
          setIsDragging(false);
        }}
        style={{
          width: '100%',
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          cursor: 'pointer',
          color: 'black',
          textDecoration: 'none',
          textAlign: 'left',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-start',
          border: '0',
          padding: '5px',
          backgroundColor: 'transparent',
          minHeight: 'var(--touch-target-size)',
        }}
        aria-label={
          showWorkspaces
            ? 'Create New Workspace'
            : showTemporarySessionButton
              ? 'Create New Temporary Session'
              : 'Create New Session'
        }
      >
        <FaPlus style={{ marginRight: '5px' }} />
        {showWorkspaces ? 'New Workspace' : showTemporarySessionButton ? 'New Temporary Session' : 'New Session'}
      </button>
    </div>
  );
};

export default NewSessionButton;
