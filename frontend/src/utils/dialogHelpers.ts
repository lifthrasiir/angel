import { apiFetch } from '../api/apiClient';

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
 * Web-based directory picker that returns the directory listing for navigation.
 * This is used by the DirectoryPicker component.
 */
export const getDirectoryListing = async (path: string = '') => {
  const url = path ? `/api/ui/directory?path=${encodeURIComponent(path)}` : '/api/ui/directory';
  const response = await apiFetch(url);
  return response.json();
};

/**
 * Opens the directory picker and returns the selected path.
 * This replaces the native directory picker functionality.
 * Uses a Promise-based approach instead of callback atoms.
 */
export const callDirectoryPicker = async (setIsPickingDirectory: (value: boolean) => void): Promise<string | null> => {
  return new Promise((resolve) => {
    // Set up a one-time event listener for the custom event
    const handleDirectorySelected = (event: CustomEvent) => {
      const selectedPath = event.detail;

      if (selectedPath) {
        resolve(selectedPath);
      } else {
        // User cancelled the directory picker - resolve with null
        resolve(null);
      }

      // Clean up the event listener
      window.removeEventListener('directorySelected', handleDirectorySelected as EventListener);

      // Close the directory picker
      setIsPickingDirectory(false);
    };

    // Add event listener for directory selection
    window.addEventListener('directorySelected', handleDirectorySelected as EventListener);

    // Open the directory picker
    setIsPickingDirectory(true);
  });
};

/**
 * Confirms the selected directory path.
 * This is used by the DirectoryPicker component.
 */
export const confirmDirectorySelection = async (selectedPath: string): Promise<PickDirectoryAPIResponse> => {
  try {
    const response = await apiFetch('/api/ui/directory', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ selectedPath }),
    });
    return await response.json();
  } catch (error: any) {
    console.error('Failed to confirm directory selection:', error);
    return { result: ResultType.Error, error: error.message };
  }
};
