import type React from 'react';
import { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';

const SessionRedirector: React.FC = () => {
  const { sessionId } = useParams<{ sessionId: string }>();
  const navigate = useNavigate();

  useEffect(() => {
    if (sessionId) {
      navigate(`/${sessionId}`, { replace: true });
    }
  }, [sessionId, navigate]);

  return null; // This component doesn't render anything, it just redirects
};

export default SessionRedirector;
