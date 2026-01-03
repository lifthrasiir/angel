import React, { useEffect } from 'react';
import { useSetAtom } from 'jotai';
import { apiFetch } from '../api/apiClient';
import { hasConnectedAccountsAtom, hasApiKeysAtom, isAuthenticatedAtom } from '../atoms/systemAtoms';
import ChatLayout from '../components/ChatLayout';

interface SessionPageProps {
  isTemporary?: boolean;
}

export const SessionPage: React.FC<SessionPageProps> = ({ isTemporary = false }) => {
  const setHasConnectedAccounts = useSetAtom(hasConnectedAccountsAtom);
  const setHasApiKeys = useSetAtom(hasApiKeysAtom);
  const setIsAuthenticated = useSetAtom(isAuthenticatedAtom);

  useEffect(() => {
    const checkAuthentication = async () => {
      try {
        // Check Google OAuth accounts
        const accountsResponse = await apiFetch('/api/accounts');
        const hasGoogleAccounts = accountsResponse.ok ? (await accountsResponse.json()).length > 0 : false;
        setHasConnectedAccounts(hasGoogleAccounts);

        // Check API keys (both OpenAI and Gemini API)
        const [openaiResponse, geminiResponse] = await Promise.all([
          apiFetch('/api/openai-configs'),
          apiFetch('/api/gemini-api-configs'),
        ]);

        let hasAnyApiKeys = false;

        if (openaiResponse.ok) {
          const openaiConfigs = await openaiResponse.json();
          hasAnyApiKeys = hasAnyApiKeys || openaiConfigs.some((config: any) => config.enabled);
        }

        if (geminiResponse.ok) {
          const geminiConfigs = await geminiResponse.json();
          hasAnyApiKeys = hasAnyApiKeys || geminiConfigs.some((config: any) => config.enabled);
        }

        setHasApiKeys(hasAnyApiKeys);

        // Set authenticated if either Google accounts or API keys are available
        setIsAuthenticated(hasGoogleAccounts || hasAnyApiKeys);
      } catch (error) {
        console.error('Failed to check authentication:', error);
        setHasConnectedAccounts(false);
        setHasApiKeys(false);
        setIsAuthenticated(false);
      }
    };

    checkAuthentication();
  }, [setHasConnectedAccounts, setHasApiKeys, setIsAuthenticated]);

  // Display loading spinner while checking authentication or loading session.
  // Assuming ChatLayout handles all loading states.
  return <ChatLayout isTemporary={isTemporary} />;
};
