/**
 * Utility functions for drag and drop file handling
 */

export interface DragData {
  isMessageAttachment?: boolean;
  blobHash?: string;
  fileName?: string;
  fileType?: string;
  isExistingAttachment?: boolean;
}

/**
 * Extract files from drag and drop event
 * @param e Drag event
 * @returns Array of File objects
 */
export async function extractFilesFromDrop(e: React.DragEvent): Promise<File[]> {
  const dragData = e.dataTransfer.getData('application/json');
  let filesToAdd: File[] = [];

  if (dragData) {
    try {
      const parsed: DragData = JSON.parse(dragData);

      if (parsed.isMessageAttachment) {
        // Handle message attachment - download it as File
        const blobUrl = `/api/blob/${parsed.blobHash}`;
        try {
          const response = await fetch(blobUrl);
          if (response.ok) {
            const blob = await response.blob();
            const file = new File([blob], parsed.fileName!, { type: parsed.fileType });
            filesToAdd = [file];
          }
        } catch (error) {
          console.error('Error downloading message attachment:', error);
          return [];
        }
      }
    } catch {
      // If parsing fails, fall back to dataTransfer files
      console.log('Failed to parse dragData, falling back to files');
    }
  }

  // Handle external files or fallback
  if (filesToAdd.length === 0 && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
    filesToAdd = Array.from(e.dataTransfer.files);
  }

  return filesToAdd;
}
