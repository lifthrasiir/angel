import React, { useEffect, useState } from 'react';

interface ToastMessageProps {
  message: string | null;
  onClose: () => void;
}

const ToastMessage: React.FC<ToastMessageProps> = ({ message, onClose }) => {
  const [isVisible, setIsVisible] = useState(false);

  useEffect(() => {
    if (message) {
      setIsVisible(true);
      const timer = setTimeout(() => {
        setIsVisible(false);
        onClose();
      }, 3000); // Hide after 3 seconds
      return () => clearTimeout(timer);
    } else {
      setIsVisible(false);
    }
  }, [message, onClose]);

  if (!isVisible) return null;

  return <div className="toast-message">{message}</div>;
};

export default ToastMessage;
