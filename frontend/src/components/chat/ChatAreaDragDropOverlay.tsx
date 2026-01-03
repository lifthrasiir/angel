import type React from 'react';
import { useState } from 'react';

interface ChatAreaDragDropOverlayProps {
  onFilesSelected: (files: File[]) => void;
  children: React.ReactNode;
}

export const ChatAreaDragDropOverlay: React.FC<ChatAreaDragDropOverlayProps> = ({ onFilesSelected, children }) => {
  const [isDragging, setIsDragging] = useState(false);

  const handleDragOver = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);
  };

  const handleDrop = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    setIsDragging(false);

    // Check if this is a message file being dropped
    const dragData = e.dataTransfer.getData('application/json');
    if (dragData) {
      try {
        const parsed = JSON.parse(dragData);
        if (parsed.isMessageAttachment || parsed.isExistingAttachment) {
          // Message files dropped on ChatArea should do nothing
          return;
        }
      } catch {
        // If parsing fails, continue with normal file handling
      }
    }

    // Only handle external files dropped on ChatArea
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      onFilesSelected(Array.from(e.dataTransfer.files));
    }
  };

  return (
    <div
      style={{
        flexGrow: 1,
        flexShrink: 1,
        flexBasis: 'auto',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        position: 'relative',
      }}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
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
            pointerEvents: 'none',
            zIndex: 10,
          }}
        />
      )}
      {children}
    </div>
  );
};

export default ChatAreaDragDropOverlay;
