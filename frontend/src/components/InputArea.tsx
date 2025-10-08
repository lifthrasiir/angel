import type React from 'react';
import ChatInput from './ChatInput';
import FileAttachmentPreview from './FileAttachmentPreview';
import TokenCountMeter from './TokenCountMeter';
import { extractFilesFromDrop } from '../utils/dragDropUtils';

interface InputAreaProps {
  handleSendMessage: () => void;
  onFilesSelected: (files: File[]) => void;
  handleRemoveFile: (index: number) => void;
  handleCancelStreaming: () => void;
  chatInputRef: React.RefObject<HTMLTextAreaElement>;
  sessionId: string | null;
  selectedFiles: File[];
}

const InputArea: React.FC<InputAreaProps> = ({
  handleSendMessage,
  onFilesSelected,
  handleRemoveFile,
  handleCancelStreaming,
  chatInputRef,
  sessionId,
  selectedFiles,
}) => {
  const handleDragOver = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();

    // Show overlay
    const overlay = document.getElementById('input-area-drag-overlay');
    if (overlay) {
      (overlay as HTMLDivElement).style.opacity = '1';
    }
  };

  const handleDrop = async (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();

    // Hide overlay
    const overlay = document.getElementById('input-area-drag-overlay');
    if (overlay) {
      (overlay as HTMLDivElement).style.opacity = '0';
    }

    const filesToAdd = await extractFilesFromDrop(e);

    // Only handle files dropped on InputArea
    if (filesToAdd.length > 0) {
      onFilesSelected(filesToAdd);
    }
  };

  const handleDragLeave = (e: React.DragEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();

    // Hide overlay when leaving the input area
    const overlay = document.getElementById('input-area-drag-overlay');
    if (overlay) {
      (overlay as HTMLDivElement).style.opacity = '0';
    }
  };

  return (
    <div onDragOver={handleDragOver} onDragLeave={handleDragLeave} onDrop={handleDrop} style={{ position: 'relative' }}>
      {/* Drag overlay */}
      <div
        id="input-area-drag-overlay"
        style={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          bottom: 0,
          backgroundColor: 'rgba(0, 123, 255, 0.1)',
          border: '2px solid rgba(0, 123, 255, 0.3)',
          pointerEvents: 'none',
          zIndex: 5,
          opacity: 0,
          transition: 'opacity 0.2s ease-in-out',
        }}
      />
      {selectedFiles.length > 0 && (
        <div
          style={{
            padding: 'calc(var(--spacing-unit) * 0.3) var(--spacing-unit)',
            borderTop: '1px solid #eee',
            background: '#f9f9f9',
            display: 'flex',
            flexWrap: 'wrap',
            gap: 'calc(var(--spacing-unit) * 0.3)',
          }}
        >
          {selectedFiles.map((file, index) => (
            <FileAttachmentPreview key={index} file={file} onRemove={() => handleRemoveFile(index)} draggable={false} />
          ))}
        </div>
      )}
      <TokenCountMeter />
      <ChatInput
        handleSendMessage={handleSendMessage}
        onFilesSelected={onFilesSelected}
        handleCancelStreaming={handleCancelStreaming}
        inputRef={chatInputRef}
        sessionId={sessionId}
      />
    </div>
  );
};

export default InputArea;
