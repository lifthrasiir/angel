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
  primaryBranchId: string,
  beforeMessageId?: string,
  fetchLimit?: number,
): Promise<any> {
  let url = `/api/chat/${sessionId}?primaryBranchId=${primaryBranchId}`;
  if (beforeMessageId) {
    url += `&beforeMessageId=${beforeMessageId}`;
  }
  if (fetchLimit) {
    url += `&fetchLimit=${fetchLimit}`;
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
