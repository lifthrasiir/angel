// Define ResultType from backend's handlers_ui.go
export enum ResultType {
  Success = 'success',
  Canceled = 'canceled',
  AlreadyOpen = 'already_open',
  Error = 'error',
}

export interface PickDirectoryAPIResponse {
  selectedPath?: string;
  result: ResultType;
  error?: string;
}

/**
 * Calls the backend API to open a native directory picker dialog.
 * Manages the UI overlay state (isPickingDirectoryAtom) during the dialog interaction.
 * @returns {Promise<PickDirectoryAPIResponse>} A promise that resolves with the dialog result.
 */
export const callNativeDirectoryPicker = async (
  setIsPickingDirectory: (value: boolean) => void,
  setStatusMessage: (message: string | null) => void,
): Promise<PickDirectoryAPIResponse> => {
  setStatusMessage('Opening directory picker...');
  setIsPickingDirectory(true); // Set state to true before opening dialog
  try {
    const response = await fetch('/api/ui/directory', {
      method: 'POST',
    });
    const data: PickDirectoryAPIResponse = await response.json();
    return data;
  } catch (error: any) {
    console.error('Failed to open directory picker:', error);
    setStatusMessage(`Failed to open directory picker: ${error.message}`);
    return { result: ResultType.Error, error: error.message };
  } finally {
    setIsPickingDirectory(false); // Always reset state after dialog interaction
  }
};
