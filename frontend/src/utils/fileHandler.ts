import { FileAttachment } from '../types/chat';

export const convertFilesToAttachments = async (files: File[]): Promise<FileAttachment[]> => {
  return Promise.all(
    files.map(async (file) => {
      const data = await new Promise<string>((resolve) => {
        const reader = new FileReader();
        reader.onloadend = () => {
          resolve(reader.result?.toString().split(',')[1] || '');
        };
        reader.readAsDataURL(file);
      });
      return { fileName: file.name, mimeType: file.type, data };
    }),
  );
};

export const handleFilesSelected = (currentFiles: File[], newFiles: File[]): File[] => {
  return [...currentFiles, ...newFiles];
};

export const handleRemoveFile = (currentFiles: File[], index: number): File[] => {
  return currentFiles.filter((_, i) => i !== index);
};
