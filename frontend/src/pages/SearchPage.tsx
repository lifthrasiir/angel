import type React from 'react';
import { useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiFetch } from '../api/apiClient';
import { FaSearch, FaSpinner } from 'react-icons/fa';

interface SearchResult {
  message_id: number;
  session_id: string;
  text: string;
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
  const [isLoading, setIsLoading] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [maxId, setMaxId] = useState<number | null>(null);

  const handleSearch = useCallback(
    async (reset: boolean = true) => {
      if (!query.trim()) return;

      setIsLoading(true);

      try {
        const requestBody: any = {
          query: query.trim(),
          limit: 20,
        };

        if (!reset && maxId) {
          requestBody.max_id = maxId;
        }

        const response = await apiFetch('/api/search', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(requestBody),
        });

        const data: SearchResponse = await response.json();

        if (reset) {
          setResults(data.results);
        } else {
          setResults((prev) => [...prev, ...data.results]);
        }

        setHasMore(data.has_more);

        if (data.results.length > 0) {
          setMaxId(data.results[data.results.length - 1].message_id);
        }
      } catch (error) {
        console.error('Search failed:', error);
      } finally {
        setIsLoading(false);
      }
    },
    [query, maxId],
  );

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    handleSearch(true);
  };

  const loadMore = () => {
    if (hasMore && !isLoading) {
      handleSearch(false);
    }
  };

  const handleResultClick = (result: SearchResult) => {
    navigate(`/${result.session_id}`);
  };

  const formatText = (text: string, maxLength: number = 200) => {
    if (text.length <= maxLength) return text;
    return text.substring(0, maxLength) + '...';
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
          <button type="submit" disabled={!query.trim() || isLoading} className="search-page-button">
            {isLoading ? (
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
        {results.length === 0 && !isLoading && query && (
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
              <div className="search-result-content">{formatText(result.text)}</div>

              {/* Workspace info */}
              {result.workspace_id && <div className="search-result-workspace">Workspace: {result.workspace_id}</div>}
            </div>
          ))}
        </div>

        {/* Load More Button */}
        {hasMore && (
          <div className="search-page-load-more">
            <button onClick={loadMore} disabled={isLoading} className="search-page-load-more-button">
              {isLoading ? (
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
