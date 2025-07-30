import React, { useState, useEffect } from 'react';
import { FaDownload, FaTimes, FaFile } from 'react-icons/fa';
import { FileAttachment } from '../types/chat';

interface FileAttachmentPreviewProps {
  file: File | FileAttachment;
  onRemove?: (file: File) => void; // Callback for removing uploaded files
}

const FileAttachmentPreview: React.FC<FileAttachmentPreviewProps> = ({ file, onRemove }) => {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const fileName = file instanceof File ? file.name : file.fileName;
  const mimeType = file instanceof File ? file.type : file.mimeType;
  const isImage = mimeType.startsWith('image/');
  const data = file instanceof File ? null : file.data; // Data is directly available for FileAttachment

  useEffect(() => {
    if (file instanceof File) {
      // For uploaded File objects, read as Data URL for preview
      const reader = new FileReader();
      reader.onloadend = () => {
        setPreviewUrl(reader.result as string);
      };
      reader.readAsDataURL(file);
    } else if (isImage && data) {
      // For FileAttachment objects (from messages), use existing base64 data
      setPreviewUrl(`data:${mimeType};base64,${data}`);
    } else {
      setPreviewUrl(null); // No preview for non-image FileAttachment
    }
  }, [file, isImage, data, mimeType]);

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
    } else if (data) {
      // For attached files (FileAttachment), use base64 data
      const url = `data:${mimeType};base64,${data}`;
      const a = document.createElement('a');
      a.href = url;
      a.download = fileName;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
    }
  };

  return (
    <div className="file-attachment-preview">
      {isImage && previewUrl ? (
        <img src={previewUrl} alt={fileName} />
      ) : (
        <span className="file-icon"><FaFile /></span> // Generic file icon
      )}
      <span className="file-name">{fileName}</span>
      <button onClick={handleDownload}>
        <FaDownload />
      </button>
      {onRemove && (
        <button onClick={() => onRemove(file as File)} className="remove-button">
          <FaTimes />
        </button>
      )}
    </div>
  );
};

export default FileAttachmentPreview;
