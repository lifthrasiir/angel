let csrfToken: string | null = null;

function getCsrfToken(): string | null {
  if (csrfToken === null) {
    const metaTag = document.querySelector('meta[name="csrf-token"]');
    if (metaTag) {
      csrfToken = metaTag.getAttribute('content');
    }
  }
  return csrfToken;
}

interface RequestOptions extends RequestInit {
  signal?: AbortSignal;
}

export async function apiFetch(input: RequestInfo | URL, init?: RequestOptions): Promise<Response> {
  const method = init?.method?.toUpperCase() || 'GET';
  const headers = new Headers(init?.headers);

  // Add CSRF token for non-GET requests
  if (method !== 'GET' && method !== 'HEAD') {
    const token = getCsrfToken();
    if (token) {
      headers.set('X-CSRF-Token', token);
    } else {
      console.warn('CSRF token not found. Request might be blocked.');
      // Optionally, throw an error or handle this case more strictly
    }
  }

  const response = await fetch(input, { ...init, headers, signal: init?.signal });

  // You can add global error handling here if needed
  if (!response.ok) {
    console.error(`API Error: ${response.status} ${response.statusText}`, await response.text());
    // Optionally, throw a custom error or return a specific error object
  }

  return response;
}

export async function fetchSessionHistory(
  sessionId: string,
  beforeMessageId?: string,
  fetchLimit?: number,
): Promise<any> {
  let url = `/api/chat/${sessionId}`;
  if (beforeMessageId) {
    url += `?beforeMessageId=${beforeMessageId}`;
  }
  if (fetchLimit) {
    url += `${beforeMessageId ? '&' : '?'}fetchLimit=${fetchLimit}`;
  }

  const response = await apiFetch(url);
  if (!response.ok) {
    throw new Error(`Failed to fetch session history: ${response.status} ${response.statusText}`);
  }
  return response.json();
}

export async function switchBranch(sessionId: string, newPrimaryBranchId: string): Promise<void> {
  const response = await apiFetch(`/api/chat/${sessionId}/branch`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      newPrimaryBranchId,
    }),
  });

  if (!response.ok) {
    throw new Error(`Failed to switch branch: ${response.status} ${response.statusText}`);
  }
}

export interface ExtractResponse {
  status: string;
  sessionId: string;
  sessionName: string;
  link: string;
  message: string;
}

export async function extractSession(sessionId: string, messageId: string): Promise<ExtractResponse> {
  const response = await apiFetch(`/api/chat/${sessionId}/extract`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      messageId,
    }),
  });

  if (!response.ok) {
    throw new Error(`Failed to extract session: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

// Account Details Types
export interface AccountDetailsResponse {
  source: 'models' | 'quota';
  models: Record<string, ModelDetails>;
}

export interface ModelDetails {
  displayName?: string;
  maxTokens?: number;
  maxOutputTokens?: number;
  supportsImages?: boolean;
  supportsThinking?: boolean;
  supportsVideo?: boolean;
  thinkingBudget?: number;
  minThinkingBudget?: number;
  recommended?: boolean;
  tokenizerType?: string;
  model?: string;
  apiProvider?: string;
  modelProvider?: string;
  quotaInfo?: QuotaInfo;
  usages?: string[];
}

export interface QuotaInfo {
  remainingFraction: number;
  resetTime: string; // ISO 8601 timestamp
}

// Fetch detailed information about an OAuth account
export async function fetchAccountDetails(accountId: number, signal?: AbortSignal): Promise<AccountDetailsResponse> {
  const response = await apiFetch(`/api/accounts/${accountId}/details`, { signal });
  if (!response.ok) {
    throw new Error(`Failed to fetch account details: ${response.status} ${response.statusText}`);
  }
  return response.json();
}
