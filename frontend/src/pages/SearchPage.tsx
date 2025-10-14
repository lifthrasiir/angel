import type React from 'react';
import { useState, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { FaSearch, FaSpinner } from 'react-icons/fa';

interface SearchResult {
  message_id: number;
  session_id: string;
  excerpt: string; // FTS5 snippet with <mark> tags
  type: string;
  created_at: string;
  session_name: string;
  workspace_id?: string;
}

interface SearchResponse {
  results: SearchResult[];
  has_more: boolean;
}

const SearchPage: React.FC = () => {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchResult[]>([]);
  const [isInitialLoading, setIsInitialLoading] = useState(false);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(false);

  const loadMoreCallbackRef = useRef<(() => void) | null>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  const search = useCallback(async (keywords: string) => {
    if (!keywords.trim()) return;

    // Cancel previous search if still running
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    // Setup new abort controller
    const abortController = new AbortController();
    abortControllerRef.current = abortController;

    setIsInitialLoading(true);
    setResults([]);
    setHasMore(false);

    let maxId: number | null = null;
    let isFirstRequest = true;

    try {
      while (true) {
        // Set appropriate loading state
        if (isFirstRequest) {
          setIsInitialLoading(true);
          isFirstRequest = false;
        } else {
          setIsLoadingMore(true);
        }

        const requestBody: any = {
          query: keywords.trim(),
          limit: 20,
        };

        if (maxId) {
          requestBody.max_id = maxId;
        }

        const response = await apiFetch('/api/search', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(requestBody),
          signal: abortController.signal,
        });

        const data: SearchResponse = await response.json();

        // Check if request was aborted
        if (abortController.signal.aborted) {
          return;
        }

        // Update results and hasMore
        setResults((prev) => [...prev, ...(data.results || [])]);
        setHasMore(data.has_more);

        if (!data.has_more || (data.results && data.results.length === 0)) {
          break;
        }

        if (data.results && data.results.length > 0) {
          maxId = data.results[data.results.length - 1].message_id;
        }

        // Clear loading state while waiting for user interaction
        setIsInitialLoading(false);
        setIsLoadingMore(false);

        // Wait for loadMore callback to be called
        await new Promise<void>((resolve) => {
          loadMoreCallbackRef.current = resolve;
        });

        // Check if search was aborted while waiting
        if (abortController.signal.aborted) {
          return;
        }
      }
    } catch (error: any) {
      if (error.name === 'AbortError') {
        // Request was cancelled, don't show error
        return;
      }
      console.error('Search failed:', error);
    } finally {
      // Clear the callback and abort controller when done
      loadMoreCallbackRef.current = null;
      abortControllerRef.current = null;
      setIsInitialLoading(false);
      setIsLoadingMore(false);
    }
  }, []);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    search(query);
  };

  const loadMore = () => {
    if (hasMore && loadMoreCallbackRef.current) {
      loadMoreCallbackRef.current();
    }
  };

  const handleResultClick = (result: SearchResult) => {
    navigate(`/${result.session_id}`);
  };

  // Render excerpt that's already properly escaped from SQL
  const renderExcerpt = (excerpt: string) => {
    return { __html: excerpt };
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleString();
  };

  return (
    <div className="search-page-container">
      {/* Header */}
      <div className="search-page-header">
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            marginBottom: '20px',
          }}
        >
          <h1
            style={{
              margin: '0 0 0 15px',
              fontSize: '24px',
              color: '#333',
            }}
          >
            Search Messages
          </h1>
        </div>

        {/* Search Form */}
        <form onSubmit={handleSubmit} className="search-page-form">
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search in messages..."
            className="search-page-input"
          />
          <button type="submit" disabled={!query.trim() || isInitialLoading} className="search-page-button">
            {isInitialLoading ? (
              <>
                <FaSpinner className="animate-spin" />
                Searching...
              </>
            ) : (
              <>
                <FaSearch />
                Search
              </>
            )}
          </button>
        </form>
      </div>

      {/* Results */}
      <div className="search-page-results">
        {results.length === 0 && !isInitialLoading && query && (
          <div className="search-page-empty">
            <FaSearch size={48} style={{ marginBottom: '16px', opacity: 0.5 }} />
            <p>No results found for "{query}"</p>
          </div>
        )}

        {results.length === 0 && !query && (
          <div className="search-page-empty">
            <FaSearch size={48} style={{ marginBottom: '16px', opacity: 0.5 }} />
            <p>Enter a search query to find messages</p>
          </div>
        )}

        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: '16px',
          }}
        >
          {results.map((result) => (
            <div key={result.message_id} onClick={() => handleResultClick(result)} className="search-result-item">
              {/* Header */}
              <div className="search-result-header">
                <div className="search-result-session-info">
                  <span className="search-result-session-name">{result.session_name || 'Untitled Session'}</span>
                  <span className="search-result-meta">
                    {result.type === 'user' ? 'User' : 'Assistant'} • {formatDate(result.created_at)}
                  </span>
                </div>
                <span className="search-result-id">#{result.message_id}</span>
              </div>

              {/* Content */}
              <div className="search-result-content" dangerouslySetInnerHTML={renderExcerpt(result.excerpt)} />

              {/* Workspace info */}
              {result.workspace_id && <div className="search-result-workspace">Workspace: {result.workspace_id}</div>}
            </div>
          ))}
        </div>

        {/* Load More Button */}
        {hasMore && (
          <div className="search-page-load-more">
            <button onClick={loadMore} disabled={isLoadingMore} className="search-page-load-more-button">
              {isLoadingMore ? (
                <>
                  <FaSpinner className="animate-spin" />
                  Loading...
                </>
              ) : (
                'Load More'
              )}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};

export default SearchPage;
