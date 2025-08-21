import React from 'react';
import { useAtomValue } from 'jotai';
import { isPickingDirectoryAtom } from '../atoms/chatAtoms';

const GlobalDialogOverlay: React.FC = () => {
  const isPickingDirectory = useAtomValue(isPickingDirectoryAtom);

  if (!isPickingDirectory) {
    return null; // Don't render if the dialog is not active
  }

  return (
    <div
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        width: '100%',
        height: '100%',
        backgroundColor: 'rgba(0, 0, 0, 0.5)', // Dark overlay
        backdropFilter: 'blur(5px)', // Blur effect
        zIndex: 1000, // Ensure it's on top of everything
        display: 'flex',
        flexDirection: 'column',
        justifyContent: 'center',
        alignItems: 'center',
        color: 'white',
        fontSize: '24px',
        textAlign: 'center',
        padding: '20px',
      }}
    >
      <p>Please select a directory in the native dialog.</p>
      <p style={{ fontSize: '16px', marginTop: '10px' }}>
        (If not visible, check your taskbar/dock or other open windows.)
      </p>
    </div>
  );
};

export default GlobalDialogOverlay;
