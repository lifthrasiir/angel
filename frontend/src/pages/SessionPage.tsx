import React, { useEffect } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSetAtom } from 'jotai';
import { fetchUserInfo } from '../utils/userManager';
import { userEmailAtom } from '../atoms/chatAtoms';
import ChatLayout from '../components/ChatLayout';

export const SessionPage: React.FC = () => {
  const navigate = useNavigate();
  const setUserEmail = useSetAtom(userEmailAtom);
  const { sessionId, workspaceId } = useParams<{ sessionId?: string; workspaceId?: string }>();

  useEffect(() => {
    const checkLoginAndLoadSession = async () => {
      try {
        const userInfo = await fetchUserInfo();
        if (userInfo && userInfo.success && userInfo.email) {
          setUserEmail(userInfo.email);
          // Session loading logic will be handled inside ChatLayout
        } else {
          // Login failed or no user info
          navigate('/login', { state: { from: window.location.pathname + window.location.search } });
        }
      } catch (error) {
        console.error('Failed to fetch user info:', error);
        navigate('/login', { state: { from: window.location.pathname + window.location.search } });
      }
    };

    checkLoginAndLoadSession();
  }, [navigate, setUserEmail, sessionId, workspaceId]);

  // Display loading spinner while checking login or loading session.
  // Assuming ChatLayout handles all loading states.
  return <ChatLayout />;
};
