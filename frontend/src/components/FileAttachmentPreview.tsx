import type React from 'react';
import { useEffect, useState } from 'react';
import { apiFetch } from '../api/apiClient';
import { FaDownload, FaFile, FaTimes } from 'react-icons/fa';
import type { FileAttachment } from '../types/chat';

interface FileAttachmentPreviewProps {
  file: File | FileAttachment;
  onRemove?: (file: File) => void;
  messageId?: string;
  sessionId?: string;
  blobIndex?: number;
  isImageOnlyMessage?: boolean;
  draggable?: boolean;
}

const FileAttachmentPreview: React.FC<FileAttachmentPreviewProps> = ({
  file,
  onRemove,
  messageId,
  sessionId,
  blobIndex,
  isImageOnlyMessage = false,
  draggable = true,
}) => {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const fileName = file instanceof File ? file.name : file.fileName;
  const mimeType = file instanceof File ? file.type : file.mimeType;
  const isImage = mimeType.startsWith('image/');
  // For FileAttachment, data is now optional and used only for upload.
  // For display/download, we use the hash and fetch from the backend.
  const fileAttachment = file instanceof File ? null : file;

  useEffect(() => {
    let objectUrl: string | null = null;

    const loadPreview = async () => {
      if (file instanceof File) {
        // For uploaded File objects, read as Data URL for preview
        const reader = new FileReader();
        reader.onloadend = () => {
          setPreviewUrl(reader.result as string);
        };
        reader.readAsDataURL(file);
      } else if (isImage && messageId && sessionId && blobIndex !== undefined) {
        // For FileAttachment objects (from messages), fetch from backend for image preview
        const blobUrl = `/api/chat/${sessionId}/blob/${messageId}.${blobIndex}`;
        try {
          const response = await apiFetch(blobUrl);
          if (response.ok) {
            const blob = await response.blob();
            objectUrl = URL.createObjectURL(blob);
            setPreviewUrl(objectUrl);
          } else {
            console.error(`Failed to fetch blob for preview: ${response.statusText}`);
            setPreviewUrl(null);
          }
        } catch (error) {
          console.error('Error fetching blob for preview:', error);
          setPreviewUrl(null);
        }
      } else {
        setPreviewUrl(null); // No preview for non-image FileAttachment or missing info
      }
    };

    loadPreview();

    return () => {
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl); // Clean up object URL on unmount
      }
    };
  }, [file, isImage, messageId, sessionId, blobIndex, mimeType]);

  const handleDownload = () => {
    if (file instanceof File) {
      // For uploaded files, create a URL from the File object
      const url = URL.createObjectURL(file);
      const a = document.createElement('a');
      a.href = url;
      a.download = fileName;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url); // Clean up
    } else if (fileAttachment && messageId && sessionId && blobIndex !== undefined) {
      // For attached files (FileAttachment), use the backend endpoint
      const downloadUrl = `/api/chat/${sessionId}/blob/${messageId}.${blobIndex}`;
      const a = document.createElement('a');
      a.href = downloadUrl;
      a.download = fileName; // Suggest download filename
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
    }
  };

  const handleDragStart = (e: React.DragEvent<HTMLDivElement>) => {
    if (!draggable) return;

    if (file instanceof File) {
      // For File objects (ChatArea attachments), set effectAllowed to 'move'
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', file.name);
      e.dataTransfer.setData(
        'application/json',
        JSON.stringify({
          isExistingAttachment: true,
          fileName: file.name,
          fileSize: file.size,
          fileType: file.type,
        }),
      );
    } else {
      // For FileAttachment objects (message attachments), we need to create a File-like drag
      e.dataTransfer.effectAllowed = 'copy';
      e.dataTransfer.setData('text/plain', file.fileName);
      e.dataTransfer.setData(
        'application/json',
        JSON.stringify({
          isMessageAttachment: true,
          fileName: file.fileName,
          fileType: file.mimeType,
          sessionId: sessionId,
          messageId: messageId,
          blobIndex: blobIndex,
        }),
      );
    }
  };

  const handleDragEnd = () => {
    // Clean up any drag-related state if needed
  };

  const previewClassName =
    isImageOnlyMessage && isImage ? 'file-attachment-preview-image-only' : 'file-attachment-preview';

  return (
    <div
      className={previewClassName}
      draggable={draggable} // Make both File and FileAttachment objects draggable if allowed
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      style={{ cursor: 'default' }}
    >
      {isImage && previewUrl ? (
        isImageOnlyMessage ? (
          <img src={previewUrl} alt={fileName} className="image-only-message-img" />
        ) : (
          <img src={previewUrl} alt={fileName} />
        )
      ) : (
        <span className="file-icon">
          <FaFile />
        </span> // Generic file icon
      )}
      {!isImageOnlyMessage && (
        <>
          <span className="file-name">{fileName}</span>
          <button onClick={handleDownload}>
            <FaDownload />
          </button>
          {onRemove && (
            <button onClick={() => onRemove(file as File)} className="remove-button">
              <FaTimes />
            </button>
          )}
        </>
      )}
    </div>
  );
};

export default FileAttachmentPreview;
