import { useState, useEffect } from 'react';
import { shouldResizeImage } from '../utils/imageResize';

// Store resized versions alongside original files
export const useAttachmentResize = (selectedFiles: File[]) => {
  const [resizedFileMap, setResizedFileMap] = useState<Map<string, File>>(new Map());

  // Store resize states for each file
  const [fileResizeStates, setFileResizeStates] = useState<Map<string, boolean>>(new Map());

  // Store processing states for each file
  const [fileProcessingStates, setFileProcessingStates] = useState<Map<string, boolean>>(new Map());

  // Store resized version when available
  const handleResizedFileAvailable = (originalFile: File, resizedFile: File) => {
    const fileKey = `${originalFile.name}_${originalFile.size}_${originalFile.type}`;
    setResizedFileMap((prev) => new Map(prev).set(fileKey, resizedFile));
  };

  // Update file resize state
  const handleFileResizeStateChange = (file: File, shouldResize: boolean) => {
    const fileKey = `${file.name}_${file.size}_${file.type}`;
    setFileResizeStates((prev) => new Map(prev).set(fileKey, shouldResize));
  };

  // Update file processing state
  const handleFileProcessingStateChange = (file: File, isProcessing: boolean) => {
    const fileKey = `${file.name}_${file.size}_${file.type}`;
    setFileProcessingStates((prev) => new Map(prev).set(fileKey, isProcessing));
  };

  // Clean up maps when file is removed
  const handleFileRemoved = (removedFile: File) => {
    const fileKey = `${removedFile.name}_${removedFile.size}_${removedFile.type}`;

    setResizedFileMap((prev) => {
      const newMap = new Map(prev);
      newMap.delete(fileKey);
      return newMap;
    });

    setFileResizeStates((prev) => {
      const newMap = new Map(prev);
      newMap.delete(fileKey);
      return newMap;
    });

    setFileProcessingStates((prev) => {
      const newMap = new Map(prev);
      newMap.delete(fileKey);
      return newMap;
    });
  };

  // Check if send should be disabled due to pending resizes
  const isSendDisabledByResizing = (): boolean => {
    for (const file of selectedFiles) {
      if (!file.type.startsWith('image/')) continue;

      const fileKey = `${file.name}_${file.size}_${file.type}`;
      const shouldResize = fileResizeStates.get(fileKey) || false;
      const isProcessing = fileProcessingStates.get(fileKey) || false;
      const hasResizedVersion = resizedFileMap.has(fileKey);

      console.log('File check:', {
        fileKey,
        fileName: file.name,
        shouldResize,
        isProcessing,
        hasResizedVersion,
        totalFiles: selectedFiles.length,
        resizeStates: Array.from(fileResizeStates.entries()),
        processingStates: Array.from(fileProcessingStates.entries()),
      });

      // If file should be resized but either processing or no resized version available
      if (shouldResize && (isProcessing || !hasResizedVersion)) {
        return true;
      }
    }
    return false;
  };

  // Get the appropriate file (original or resized) for sending
  const getFilesForSending = (): File[] => {
    return selectedFiles.map((file) => {
      if (!file.type.startsWith('image/')) return file;

      const fileKey = `${file.name}_${file.size}_${file.type}`;
      const shouldResize = fileResizeStates.get(fileKey) || false;
      const resizedFile = resizedFileMap.get(fileKey);

      return shouldResize && resizedFile ? resizedFile : file;
    });
  };

  // Initialize resize states for new image files that need resizing
  useEffect(() => {
    const processFile = async (file: File) => {
      if (!file.type.startsWith('image/')) return;

      const fileKey = `${file.name}_${file.size}_${file.type}`;

      // Check if we already have state for this file
      if (fileResizeStates.has(fileKey)) return;

      // Get dimensions to determine if resize is needed
      try {
        const dimensions = await new Promise<any>((resolve, reject) => {
          const img = new Image();
          img.onload = () => {
            resolve({
              width: img.naturalWidth,
              height: img.naturalHeight,
            });
          };
          img.onerror = reject;
          img.src = URL.createObjectURL(file);
        });

        const needsResize = shouldResizeImage(dimensions);

        // Set initial resize state, but let the component control processing state
        setFileResizeStates((prev) => new Map(prev).set(fileKey, needsResize));
        console.log('Set resize state for', fileKey, 'needsResize:', needsResize);
      } catch (error) {
        console.error('Failed to check image dimensions:', error);
      }
    };

    selectedFiles.forEach(processFile);
  }, [selectedFiles]);

  // Clean up maps when selectedFiles changes (session switch, etc.)
  useEffect(() => {
    const currentFileKeys = new Set(selectedFiles.map((file) => `${file.name}_${file.size}_${file.type}`));

    // Clean up maps for files that are no longer in selectedFiles
    setResizedFileMap((prev) => {
      const newMap = new Map<string, File>();
      for (const [key, file] of prev) {
        if (currentFileKeys.has(key)) {
          newMap.set(key, file);
        }
      }
      return newMap;
    });

    setFileResizeStates((prev) => {
      const newMap = new Map<string, boolean>();
      for (const [key] of prev) {
        if (currentFileKeys.has(key)) {
          newMap.set(key, prev.get(key)!);
        }
      }
      return newMap;
    });

    setFileProcessingStates((prev) => {
      const newMap = new Map<string, boolean>();
      for (const [key] of prev) {
        if (currentFileKeys.has(key)) {
          newMap.set(key, prev.get(key)!);
        }
      }
      return newMap;
    });
  }, [selectedFiles]);

  return {
    handleResizedFileAvailable,
    handleFileResizeStateChange,
    handleFileProcessingStateChange,
    handleFileRemoved,
    isSendDisabledByResizing,
    getFilesForSending,
  };
};
