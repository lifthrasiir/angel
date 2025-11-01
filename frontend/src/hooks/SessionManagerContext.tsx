import React, { createContext, useContext } from 'react';
import { useSessionManager, type SessionManager } from './useSessionManager';

const SessionManagerContext = createContext<SessionManager | null>(null);

export const SessionManagerProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const sessionManager = useSessionManager();
  return <SessionManagerContext.Provider value={sessionManager}>{children}</SessionManagerContext.Provider>;
};

export const useSessionManagerContext = (): SessionManager => {
  const context = useContext(SessionManagerContext);
  if (!context) {
    throw new Error('useSessionManagerContext must be used within SessionManagerProvider');
  }
  return context;
};
