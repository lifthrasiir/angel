import { useEffect } from 'react';
import { useLocation, useParams } from 'react-router-dom';
import type { Session } from '../types/chat';

export const useDocumentTitle = (sessions: Session[]) => {
  const { sessionId: urlSessionId } = useParams();
  const location = useLocation();

  useEffect(() => {
    const currentSession = sessions.find((s) => s.id === urlSessionId);
    if (location.pathname === '/new') {
      document.title = 'Angel';
    } else if (currentSession && currentSession.name) {
      document.title = `Angel: ${currentSession.name}`;
    } else if (urlSessionId) {
      document.title = 'Angel: Loading...';
    }
  }, [urlSessionId, sessions, location.pathname]);
};
