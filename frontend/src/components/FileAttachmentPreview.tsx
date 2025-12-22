import type React from 'react';
import { useEffect, useState } from 'react';
import { FaDownload, FaFile, FaTimes, FaCompress, FaExpand } from 'react-icons/fa';
import type { FileAttachment } from '../types/chat';
import {
  getImageDimensions,
  shouldResizeImage,
  resizeImage,
  calculateTileConstrainedDimensions,
  resizedBlobToFile,
  type ImageDimensions,
  type ImageResizeResult,
} from '../utils/imageResize';
import './FileAttachmentPreview.css';

interface FileAttachmentPreviewProps {
  file: File | FileAttachment;
  onRemove?: (file: File) => void;
  onResize?: (originalFile: File, resizedFile: File) => void;
  onResizeStateChange?: (file: File, shouldResize: boolean) => void;
  onProcessingStateChange?: (file: File, isProcessing: boolean) => void;
  isImageOnlyMessage?: boolean;
  draggable?: boolean;
}

const FileAttachmentPreview: React.FC<FileAttachmentPreviewProps> = ({
  file,
  onRemove,
  onResize,
  onResizeStateChange,
  onProcessingStateChange,
  isImageOnlyMessage = false,
  draggable = true,
}) => {
  const [previewUrl, setPreviewUrl] = useState<string | null>(null);
  const [imageDimensions, setImageDimensions] = useState<ImageDimensions | null>(null);
  const [shouldResize, setShouldResize] = useState(false);

  const [isResizing, setIsResizing] = useState(false);
  const [resizeResult, setResizeResult] = useState<ImageResizeResult | null>(null);
  const fileName = file instanceof File ? file.name : file.fileName;
  const mimeType = file instanceof File ? file.type : file.mimeType;
  const isImage = mimeType.startsWith('image/');
  // For FileAttachment, data is now optional and used only for upload.
  // For display/download, we use the hash and fetch from the backend.
  const fileAttachment = file instanceof File ? null : file;

  // Helper function to get URL for FileAttachment
  const getFileAttachmentUrl = (): string | null => {
    if (!fileAttachment) return null;

    if (fileAttachment.data) {
      return `data:${fileAttachment.mimeType};base64,${fileAttachment.data}`;
    } else if (fileAttachment.hash) {
      return `/api/blob/${fileAttachment.hash}`;
    }
    return null;
  };

  useEffect(() => {
    const loadPreview = async () => {
      if (file instanceof File) {
        // For uploaded File objects, read as Data URL for preview
        const reader = new FileReader();
        reader.onloadend = () => {
          setPreviewUrl(reader.result as string);
        };
        reader.readAsDataURL(file);

        // Get image dimensions for resize functionality
        if (isImage) {
          try {
            const dimensions = await getImageDimensions(file);
            setImageDimensions(dimensions);

            // Check if image needs resizing
            const needsResize = shouldResizeImage(dimensions);

            if (needsResize) {
              setShouldResize(true);

              // Notify parent of resize state change
              if (onResizeStateChange) {
                onResizeStateChange(file, true);
              }

              // Start processing immediately if resize is needed
              if (onResize) {
                setIsResizing(true);
                if (onProcessingStateChange) {
                  onProcessingStateChange(file, true);
                }

                resizeImage(file)
                  .then((result) => {
                    setResizeResult(result);
                    // Store the resized file in the parent component's map
                    if (onResize) {
                      const resizedFile = resizedBlobToFile(result.blob, file);
                      onResize(file, resizedFile);
                    }
                  })
                  .catch((error) => {
                    console.error('Failed to resize image:', error);
                    setShouldResize(false);
                    // Notify parent component of resize state change (failed)
                    if (onResizeStateChange) {
                      onResizeStateChange(file, false);
                    }
                  })
                  .finally(() => {
                    setIsResizing(false);
                    // Notify parent component of processing state change (completed)
                    if (onProcessingStateChange) {
                      onProcessingStateChange(file, false);
                    }
                  });
              }
            } else {
              // If resize is not needed, ensure parent knows
              if (onResizeStateChange) {
                onResizeStateChange(file, false);
              }
            }
          } catch (error) {
            console.error('Failed to get image dimensions:', error);
          }
        }
      } else if (fileAttachment && isImage) {
        // For FileAttachment objects (from messages)
        setPreviewUrl(getFileAttachmentUrl());
      } else {
        setPreviewUrl(null); // No preview for non-image FileAttachment or missing info
      }
    };

    loadPreview();

    return () => {
      // No cleanup needed for direct URLs
    };
  }, [file, isImage, fileAttachment, mimeType]);

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
    } else if (fileAttachment) {
      // For attached files (FileAttachment)
      const downloadUrl = getFileAttachmentUrl();
      if (downloadUrl) {
        const a = document.createElement('a');
        a.href = downloadUrl;
        a.download = fileName; // Suggest download filename
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
      }
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
          blobHash: file.hash,
        }),
      );
    }
  };

  const handleDragEnd = () => {
    // Clean up any drag-related state if needed
  };

  const handleResizeToggle = async () => {
    if (!(file instanceof File)) return;

    const newCheckedState = !shouldResize;
    setShouldResize(newCheckedState);

    // Notify parent component of resize state change
    if (onResizeStateChange) {
      onResizeStateChange(file, newCheckedState);
    }

    // If toggling to compress state and we don't have a resized version yet, start resizing
    if (newCheckedState && !resizeResult) {
      setIsResizing(true);
      // Notify parent component of processing state change
      if (onProcessingStateChange) {
        onProcessingStateChange(file, true);
      }

      try {
        const result = await resizeImage(file);
        setResizeResult(result);
        // Store the resized file in the parent component's map
        if (onResize) {
          const resizedFile = resizedBlobToFile(result.blob, file);
          onResize(file, resizedFile);
        }
      } catch (error) {
        console.error('Failed to resize image:', error);
        setShouldResize(false);
        // Notify parent component of resize state change (failed)
        if (onResizeStateChange) {
          onResizeStateChange(file, false);
        }
      } finally {
        setIsResizing(false);
        // Notify parent component of processing state change (completed)
        if (onProcessingStateChange) {
          onProcessingStateChange(file, false);
        }
      }
    }
    // If toggling to expand state, don't clear the resize result - keep it for later use
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
          <div className="file-info">
            <span className="file-name">{fileName}</span>
          </div>
          <div className="file-actions">
            {file instanceof File &&
              isImage &&
              imageDimensions &&
              shouldResizeImage(imageDimensions) &&
              onResize &&
              (() => {
                const constrainedDimensions = calculateTileConstrainedDimensions(imageDimensions);
                return (
                  <button
                    onClick={handleResizeToggle}
                    disabled={isResizing}
                    title={
                      isResizing
                        ? 'Processing image resize...'
                        : shouldResize
                          ? `Click to enlarge from ${constrainedDimensions.width}×${constrainedDimensions.height} to ${imageDimensions.width}×${imageDimensions.height}`
                          : `Click to resize from ${imageDimensions.width}×${imageDimensions.height} to ${constrainedDimensions.width}×${constrainedDimensions.height}`
                    }
                    className="resize-toggle-button"
                  >
                    {shouldResize ? <FaCompress /> : <FaExpand />}
                  </button>
                );
              })()}
            <button onClick={handleDownload} title="Download">
              <FaDownload />
            </button>
            {onRemove && (
              <button onClick={() => onRemove(file as File)} className="remove-button" title="Remove">
                <FaTimes />
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
};

export default FileAttachmentPreview;
