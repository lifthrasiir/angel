import { apiFetch } from '../api/apiClient';

export const fetchUserInfo = async () => {
  try {
    const response = await apiFetch('/api/userinfo');
    if (response.ok) {
      const data = await response.json();
      return { email: data.email, success: true };
    } else if (response.status === 401) {
      return { email: null, success: true };
    } else {
      console.error('Failed to fetch user info:', response.status, response.statusText);
      return { email: null, success: false };
    }
  } catch (error) {
    console.error('Error fetching user info:', error);
    return { email: null, success: false };
  }
};

export const handleLogin = (currentPath: string, inputMessage: string) => {
  const draftMessage = inputMessage;
  let redirectToUrl = `/login?redirect_to=${encodeURIComponent(currentPath)}`;

  if (draftMessage) {
    redirectToUrl += `&draft_message=${encodeURIComponent(draftMessage)}`;
  }
  window.location.href = redirectToUrl;
};
