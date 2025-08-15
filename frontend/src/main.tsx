import React, { lazy, useEffect, useState } from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom';
import { Provider } from 'jotai';
import './index.css';

import ChatLayout from './components/ChatLayout';
import { SessionPage } from './pages/SessionPage';
import SessionRedirector from './components/SessionRedirector';
import ToastMessage from './components/ToastMessage.tsx';
import { WorkspaceProvider } from './hooks/WorkspaceContext';

const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const NewWorkspacePage = lazy(() => import('./pages/NewWorkspacePage'));
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'));

const router = createBrowserRouter([
  {
    path: '/',
    element: <Navigate to="/new" replace />,
  },
  {
    path: '/new',
    element: <SessionPage />,
  },
  {
    path: '/w/new',
    element: (
      <ChatLayout>
        <NewWorkspacePage />
      </ChatLayout>
    ),
  },
  {
    path: '/w/:workspaceId',
    element: <Navigate to="new" replace />,
  },
  {
    path: '/w/:workspaceId/new',
    element: <SessionPage />,
  },
  {
    path: '/w/:workspaceId/:sessionId',
    element: <SessionRedirector />,
  },
  {
    path: '/:sessionId',
    element: <SessionPage />,
  },
  {
    path: '/settings',
    element: <SettingsPage />,
  },
  {
    path: '*',
    element: <NotFoundPage />,
  },
]);

const Root = () => {
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  useEffect(() => {
    const originalOnError = window.onerror;

    window.onerror = (message, source, lineno, colno, error) => {
      const errorMessage = `An unexpected error occurred: ${message}`;
      console.error('Uncaught Error:', {
        message,
        source,
        lineno,
        colno,
        error,
      });
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
      <Provider>
        <WorkspaceProvider>
          <RouterProvider router={router} />
        </WorkspaceProvider>
      </Provider>
      <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
