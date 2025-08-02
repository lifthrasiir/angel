import React, { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';

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