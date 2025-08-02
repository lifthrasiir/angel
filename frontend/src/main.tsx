import React, { useState, useEffect, lazy } from 'react'
import ReactDOM from 'react-dom/client'
import { createBrowserRouter, RouterProvider, Navigate } from 'react-router-dom';
import './index.css'
import ToastMessage from './components/ToastMessage.tsx';
import { ChatProvider } from './hooks/ChatContext';
import { WorkspaceProvider } from './hooks/WorkspaceContext';

import ChatLayout from './components/ChatLayout';
import SessionRedirector from './components/SessionRedirector';
const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const NewWorkspacePage = lazy(() => import('./pages/NewWorkspacePage'));
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'));

const router = createBrowserRouter([
  {
    path: "/",
    element: <Navigate to="/new" replace />,
  },
  {
    path: "/new",
    element: <ChatLayout />,
  },
  {
    path: "/:sessionId",
    element: <ChatLayout />,
  },
  {
    path: "/settings",
    element: <SettingsPage />,
  },
  {
    path: "/w",
    element: <NotFoundPage />,
  },
  {
    path: "/w/new",
    element: <ChatLayout><NewWorkspacePage /></ChatLayout>,
  },
  {
    path: "/w/:workspaceId",
    element: <Navigate to="new" replace />,
  },
  {
    path: "/w/:workspaceId/new",
    element: <ChatLayout />,
  },
  {
    path: "/w/:workspaceId/:sessionId",
    element: <SessionRedirector />,
  },
  {
    path: "*",
    element: <NotFoundPage />,
  },
]);

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
      <ChatProvider>
        <WorkspaceProvider>
          <RouterProvider router={router} />
        </WorkspaceProvider>
      </ChatProvider>
      <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
