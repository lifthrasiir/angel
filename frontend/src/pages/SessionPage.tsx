import React, { useEffect } from 'react';
import { useSetAtom } from 'jotai';
import { fetchUserInfo } from '../utils/userManager';
import { userEmailAtom } from '../atoms/chatAtoms';
import ChatLayout from '../components/ChatLayout';

export const SessionPage: React.FC = () => {
  const setUserEmail = useSetAtom(userEmailAtom);

  useEffect(() => {
    const checkLoginAndSetUser = async () => {
      try {
        const userInfo = await fetchUserInfo();
        if (userInfo && userInfo.success && userInfo.email) {
          setUserEmail(userInfo.email);
        } else {
          // User not logged in - don't redirect, just continue with null email
          setUserEmail(null);
        }
      } catch (error) {
        console.error('Failed to fetch user info:', error);
        setUserEmail(null);
      }
    };

    checkLoginAndSetUser();
  }, [setUserEmail]);

  // Display loading spinner while checking login or loading session.
  // Assuming ChatLayout handles all loading states.
  return <ChatLayout />;
};
