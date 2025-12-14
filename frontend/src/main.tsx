import React, { lazy, useEffect, useState } from 'react';
import ReactDOM from 'react-dom/client';
import { createBrowserRouter, Navigate, RouterProvider, Routes, Route } from 'react-router-dom';
import { Provider } from 'jotai';
import './index.css';

import ChatLayout from './components/ChatLayout';
import { SessionPage } from './pages/SessionPage';
import SessionRedirector from './components/SessionRedirector';
import ToastMessage from './components/ToastMessage.tsx';
import { WorkspaceProvider } from './hooks/WorkspaceContext';
import { DirectoryPickerManager } from './components/DirectoryPickerManager';
import { SessionManagerProvider } from './hooks/SessionManagerContext';

import './components/tools/index.ts';

const SettingsPage = lazy(() => import('./pages/SettingsPage'));
const SearchPage = lazy(() => import('./pages/SearchPage'));
const NewWorkspacePage = lazy(() => import('./pages/NewWorkspacePage'));
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'));

const AppRoutes = () => (
  <SessionManagerProvider>
    <WorkspaceProvider>
      <React.Suspense fallback={<div>Loading...</div>}>
        <Routes>
          <Route path="/" element={<Navigate to="/new" replace />} />
          <Route path="/new" element={<SessionPage />} />
          <Route path="/temp" element={<SessionPage isTemporary={true} />} />
          <Route
            path="/w/new"
            element={
              <ChatLayout>
                <NewWorkspacePage />
              </ChatLayout>
            }
          />
          <Route path="/w/:workspaceId" element={<Navigate to="new" replace />} />
          <Route path="/w/:workspaceId/new" element={<SessionPage />} />
          <Route path="/w/:workspaceId/temp" element={<SessionPage isTemporary={true} />} />
          <Route path="/w/:workspaceId/:sessionId" element={<SessionRedirector />} />
          <Route path="/:sessionId" element={<SessionPage />} />
          <Route
            path="/search"
            element={
              <ChatLayout>
                <SearchPage />
              </ChatLayout>
            }
          />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Routes>
      </React.Suspense>
    </WorkspaceProvider>
  </SessionManagerProvider>
);

const router = createBrowserRouter([
  {
    path: '*',
    element: <AppRoutes />,
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
        <RouterProvider router={router} />
        <ToastMessage message={toastMessage} onClose={() => setToastMessage(null)} />
        <DirectoryPickerManager />
      </Provider>
    </React.StrictMode>
  );
};

ReactDOM.createRoot(document.getElementById('root')!).render(<Root />);
