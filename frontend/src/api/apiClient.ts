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
  // Add any custom options here if needed
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

  const response = await fetch(input, { ...init, headers });

  // You can add global error handling here if needed
  if (!response.ok) {
    console.error(`API Error: ${response.status} ${response.statusText}`, await response.text());
    // Optionally, throw a custom error or return a specific error object
  }

  return response;
}
