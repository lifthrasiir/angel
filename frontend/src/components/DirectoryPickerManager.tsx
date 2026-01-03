import React, { useEffect, useState } from 'react';
import { DirectoryPicker } from './DirectoryPicker';

// Global state to track initial path for directory picker
let globalInitialPath: string = '.';

// Global functions to control the directory picker
let setIsPickingDirectoryFn: ((value: boolean) => void) | null = null;

export const setDirectoryPickerInitialPath = (path: string) => {
  globalInitialPath = path;
};

export const setIsPickingDirectory = (value: boolean) => {
  if (setIsPickingDirectoryFn) {
    setIsPickingDirectoryFn(value);
  }
};

export const DirectoryPickerManager: React.FC = () => {
  const [isPickingDirectory, setIsPickingDirectoryState] = useState<boolean>(false);
  const [initialPath, setInitialPath] = useState<string>('.');

  // Store the setter function globally
  useEffect(() => {
    setIsPickingDirectoryFn = setIsPickingDirectoryState;
    return () => {
      setIsPickingDirectoryFn = null;
    };
  }, []);

  const handleClose = () => {
    setIsPickingDirectoryState(false);

    // Dispatch event with null to indicate cancellation
    window.dispatchEvent(new CustomEvent('directorySelected', { detail: null }));
  };

  const handleDirectorySelected = (path: string) => {
    setIsPickingDirectoryState(false);

    // Dispatch custom event with selected path
    window.dispatchEvent(new CustomEvent('directorySelected', { detail: path }));
  };

  // Update initial path when directory picker opens
  useEffect(() => {
    if (isPickingDirectory) {
      setInitialPath(globalInitialPath);
      // Reset to default after use
      globalInitialPath = '.';
    }
  }, [isPickingDirectory]);

  return (
    <DirectoryPicker
      isOpen={isPickingDirectory}
      onClose={handleClose}
      onDirectorySelected={handleDirectorySelected}
      initialPath={initialPath}
    />
  );
};
