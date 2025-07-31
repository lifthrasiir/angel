import React, { useState, useEffect } from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import './index.css'
import ToastMessage from './components/ToastMessage.tsx';

const Root = () => {
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  useEffect(() => {
    const originalOnError = window.onerror;

    window.onerror = (message, source, lineno, colno, error) => {
      const errorMessage = `An unexpected error occurred: ${message}`;
      console.error('Uncaught Error:', { message, source, lineno, colno, error });
      setToastMessage(errorMessage);

      if (originalOnError) {
        return originalOnError(message, source, lineno, colno, error);
      }
      return false; // Prevent default error handling
    };

    return () => {
      window.onerror = originalOnError; // Clean up on unmount
    };
  }, []);

  return (
    <React.StrictMode>
      <App />
      <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
