import type { FileAttachment } from '../types/chat';

export const convertFilesToAttachments = async (files: File[]): Promise<FileAttachment[]> => {
  return Promise.all(
    files.map(async (file) => {
      const data = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onloadend = () => {
          // reader.result will be a Data URL (e.g., "data:image/png;base64,iVBORw0...")
          // We need to extract only the base64 part
          resolve(reader.result?.toString().split(',')[1] || '');
        };
        reader.onerror = reject;
        reader.readAsDataURL(file);
      });
      return { fileName: file.name, mimeType: file.type, data }; // hash will be filled by backend
    }),
  );
};

export const handleFilesSelected = (currentFiles: File[], newFiles: File[]): File[] => {
  return [...currentFiles, ...newFiles];
};

export const handleRemoveFile = (currentFiles: File[], index: number): File[] => {
  return currentFiles.filter((_, i) => i !== index);
};
