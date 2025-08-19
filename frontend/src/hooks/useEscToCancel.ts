import { useEffect, useState } from 'react';

interface UseEscToCancelProps {
  isProcessing: boolean;
  onCancel: () => void;
}

const useEscToCancel = ({ isProcessing, onCancel }: UseEscToCancelProps) => {
  const [lastEscPressTime, setLastEscPressTime] = useState(0);
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isProcessing) {
        const currentTime = new Date().getTime();
        if (currentTime - lastEscPressTime < 1000) {
          // 1 second interval
          onCancel();
          setLastEscPressTime(0); // Reset
          setToastMessage(null); // Clear toast
        } else {
          setToastMessage('Press ESC twice quickly to cancel');
          setLastEscPressTime(currentTime);
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [isProcessing, lastEscPressTime, onCancel]);

  return { toastMessage, setToastMessage };
};

export default useEscToCancel;
