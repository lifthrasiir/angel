import React, { useEffect, useState } from 'react';
import { useAtomValue, useSetAtom } from 'jotai';
import { isPickingDirectoryAtom } from '../atoms/uiAtoms';
import { DirectoryPicker } from './DirectoryPicker';

// Global state to track initial path for directory picker
let globalInitialPath: string = '.';

export const setDirectoryPickerInitialPath = (path: string) => {
  globalInitialPath = path;
};

export const DirectoryPickerManager: React.FC = () => {
  const isPickingDirectory = useAtomValue(isPickingDirectoryAtom);
  const setIsPickingDirectory = useSetAtom(isPickingDirectoryAtom);
  const [initialPath, setInitialPath] = useState<string>('.');

  const handleClose = () => {
    setIsPickingDirectory(false);

    // Dispatch event with null to indicate cancellation
    window.dispatchEvent(new CustomEvent('directorySelected', { detail: null }));
  };

  const handleDirectorySelected = (path: string) => {
    setIsPickingDirectory(false);

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
