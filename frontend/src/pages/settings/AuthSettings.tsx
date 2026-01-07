import type React from 'react';
import { useEffect, useState, useRef } from 'react';
import { apiFetch, fetchAccountDetails, AccountDetailsResponse } from '../../api/apiClient';
import { useSetAtom } from 'jotai';
import GeminiAPISettings from '../../components/settings/GeminiAPISettings';
import { AccountDetailsModal } from '../../components/settings/AccountDetailsModal';
import { hasConnectedAccountsAtom, isAuthenticatedAtom } from '../../atoms/systemAtoms';

interface Account {
  id: number;
  email: string;
  createdAt: string;
  updatedAt: string;
  kind: string;
  hasProject: boolean;
}

const AuthSettings: React.FC = () => {
  const setHasConnectedAccounts = useSetAtom(hasConnectedAccountsAtom);
  const setIsAuthenticated = useSetAtom(isAuthenticatedAtom);

  // State for managing accounts
  const [accounts, setAccounts] = useState<Account[]>([]);

  // State for account details modal
  const [selectedAccountDetails, setSelectedAccountDetails] = useState<{
    email: string;
    details?: AccountDetailsResponse;
    isLoading: boolean;
  } | null>(null);

  // Ref to store AbortController for current fetch request
  const abortControllerRef = useRef<AbortController | null>(null);

  const fetchAccounts = async () => {
    try {
      const response = await apiFetch('/api/accounts');
      if (response.ok) {
        const data: Account[] = await response.json();
        setAccounts(data);
        // Only count accounts with valid projects as "connected"
        const activeAccounts = data.filter((account) => account.hasProject);
        setHasConnectedAccounts(activeAccounts.length > 0);
      } else {
        console.error('Failed to fetch accounts:', response.status, response.statusText);
        setHasConnectedAccounts(false);
      }
    } catch (error) {
      console.error('Error fetching accounts:', error);
      setHasConnectedAccounts(false);
    }
  };

  useEffect(() => {
    fetchAccounts();
  }, []);

  // Update authentication status whenever accounts change
  useEffect(() => {
    // Only count accounts with valid projects
    const hasValidAccounts = accounts.some((account) => account.hasProject);
    setIsAuthenticated(hasValidAccounts);
  }, [accounts, setIsAuthenticated]);

  const handleLogoutAccount = async (accountId: number, email: string) => {
    if (!window.confirm(`Are you sure you want to logout the account ${email}?`)) {
      return;
    }

    try {
      const response = await apiFetch('/api/logout', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ id: accountId }),
      });
      if (response.ok) {
        // Refresh accounts list and update connected accounts status
        await fetchAccounts();
      } else {
        console.error('Failed to logout account:', response.status, response.statusText);
        alert('Failed to logout account.');
      }
    } catch (error) {
      console.error('Error during account logout:', error);
      alert('Error occurred during account logout.');
    }
  };

  const handleLoginWithProvider = (provider: string) => {
    const currentPath = window.location.pathname + window.location.search;
    const redirectToUrl = `/login?provider=${provider}&redirect_to=${encodeURIComponent(currentPath)}`;
    window.location.href = redirectToUrl;
  };

  // Refresh accounts when the page becomes visible (user returns from login)
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'visible') {
        fetchAccounts();
      }
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, []);

  const handleViewAccountDetails = async (account: Account) => {
    // Cancel any ongoing request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    // Create new AbortController for this request
    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    // Immediately show the modal with loading state
    setSelectedAccountDetails({
      email: account.email,
      isLoading: true,
    });

    try {
      const details = await fetchAccountDetails(account.id, abortController.signal);

      // Check if request was aborted
      if (abortController.signal.aborted) {
        return;
      }

      // Update the modal with the loaded details
      setSelectedAccountDetails({
        email: account.email,
        details,
        isLoading: false,
      });
    } catch (error) {
      // Don't show error for aborted requests
      if (abortController.signal.aborted) {
        return;
      }

      console.error('Failed to fetch account details:', error);
      // Keep the modal open but show error state
      setSelectedAccountDetails({
        email: account.email,
        isLoading: false,
      });
      alert('Failed to load account details. Please try again.');
    } finally {
      // Clean up the abort controller reference
      if (abortControllerRef.current === abortController) {
        abortControllerRef.current = null;
      }
    }
  };

  const handleCloseAccountDetails = () => {
    // Cancel any ongoing request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }

    // Close the modal
    setSelectedAccountDetails(null);
  };

  // Render account list for a specific provider
  const renderAccountList = (providerAccounts: Account[], providerName: string) => {
    if (providerAccounts.length === 0) return null;

    return (
      <div style={{ flex: 1 }}>
        <div style={{ marginBottom: '15px' }}>
          <h6 style={{ margin: '0 0 8px 0', fontSize: '14px', fontWeight: 'bold' }}>{providerName}</h6>
          {providerAccounts.map((account) => (
            <div
              key={account.id}
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                padding: '8px 12px',
                margin: '4px 0',
                backgroundColor: account.hasProject ? '#f8f9fa' : '#fff3cd',
                borderRadius: '4px',
                border: `1px solid ${account.hasProject ? '#dee2e6' : '#ffeaa7'}`,
                opacity: account.hasProject ? 1 : 0.7,
              }}
            >
              <div>
                <strong
                  onClick={() => handleViewAccountDetails(account)}
                  style={{
                    cursor: 'pointer',
                  }}
                >
                  {account.email}
                </strong>
                {!account.hasProject && (
                  <div style={{ fontSize: '11px', color: '#856404', marginTop: '2px' }}>No project ID - inactive</div>
                )}
              </div>
              <div style={{ display: 'flex', gap: '8px' }}>
                <button
                  onClick={() => handleLogoutAccount(account.id, account.email)}
                  style={{
                    padding: '4px 8px',
                    fontSize: '12px',
                    backgroundColor: '#dc3545',
                    color: 'white',
                    border: 'none',
                    borderRadius: '3px',
                    cursor: 'pointer',
                  }}
                >
                  Remove
                </button>
              </div>
            </div>
          ))}
        </div>
      </div>
    );
  };

  const geminicliAccounts = accounts.filter((account) => account.kind === 'geminicli');
  const antigravityAccounts = accounts.filter((account) => account.kind === 'antigravity');
  const activeAccounts = accounts.filter((account) => account.hasProject);

  return (
    <div>
      <h3>Authentication</h3>

      {geminicliAccounts.length > 0 || antigravityAccounts.length > 0 ? (
        <div>
          <h4>
            Connected Google Accounts ({activeAccounts.length} active, {accounts.length - activeAccounts.length}{' '}
            inactive)
          </h4>
          <p style={{ fontSize: '14px', color: '#666', marginTop: '5px' }}>
            Only active accounts (with project IDs) are used for LLM operations. Inactive accounts are shown for
            reference only.
          </p>

          {/* Two-column layout for different providers */}
          <div style={{ display: 'flex', gap: '20px', marginTop: '10px' }}>
            {renderAccountList(geminicliAccounts, 'Gemini CLI')}
            {renderAccountList(antigravityAccounts, 'Antigravity')}
          </div>
        </div>
      ) : (
        <div>
          <p>No connected accounts.</p>
          <p style={{ fontSize: '14px', color: '#666', marginTop: '5px' }}>
            Add a Google account to start using the application.
          </p>
        </div>
      )}

      <div style={{ marginTop: '20px', display: 'flex', gap: '10px', alignItems: 'center' }}>
        <button
          onClick={() => handleLoginWithProvider('geminicli')}
          style={{
            padding: '8px 16px',
            backgroundColor: '#007bff',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
          }}
        >
          Add Gemini CLI Account
        </button>
        <button
          onClick={() => handleLoginWithProvider('antigravity')}
          style={{
            padding: '8px 16px',
            backgroundColor: '#007bff',
            color: 'white',
            border: 'none',
            borderRadius: '4px',
            cursor: 'pointer',
          }}
        >
          Add Antigravity Account
        </button>
      </div>

      <hr style={{ margin: '30px 0', border: 'none', borderTop: '1px solid #eee' }} />

      <GeminiAPISettings onConfigChange={() => {}} />

      {/* Account Details Modal */}
      {selectedAccountDetails && (
        <AccountDetailsModal
          accountEmail={selectedAccountDetails.email}
          details={selectedAccountDetails.details}
          isLoading={selectedAccountDetails.isLoading}
          onClose={handleCloseAccountDetails}
        />
      )}
    </div>
  );
};

export default AuthSettings;
