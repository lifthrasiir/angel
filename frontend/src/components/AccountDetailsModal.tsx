import { useState, useMemo } from 'react';
import { AccountDetailsResponse, ModelDetails, QuotaInfo } from '../api/apiClient';

interface Props {
  accountEmail: string;
  details: AccountDetailsResponse;
  onClose: () => void;
}

type SortColumn =
  | 'modelId'
  | 'displayName'
  | 'maxTokens'
  | 'maxOutputTokens'
  | 'quota'
  | 'images'
  | 'thinking'
  | 'video'
  | 'usages';
type SortDirection = 'asc' | 'desc';

interface ModelEntry extends ModelDetails {
  modelId: string;
}

export function AccountDetailsModal({ accountEmail, details, onClose }: Props) {
  const [sortColumn, setSortColumn] = useState<SortColumn>('displayName');
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc');

  const isQuotaOnly = details.source === 'quota';

  // Convert models object to sortable array
  const modelEntries = useMemo((): ModelEntry[] => {
    return Object.entries(details.models).map(([modelId, model]) => ({
      modelId,
      ...model,
    }));
  }, [details.models]);

  // Sort models
  const sortedModels = useMemo(() => {
    const sorted = [...modelEntries].sort((a, b) => {
      let comparison = 0;

      switch (sortColumn) {
        case 'modelId':
          comparison = a.modelId.localeCompare(b.modelId);
          break;
        case 'displayName':
          comparison = (a.displayName || a.modelId).localeCompare(b.displayName || b.modelId);
          break;
        case 'maxTokens':
          comparison = (a.maxTokens || 0) - (b.maxTokens || 0);
          break;
        case 'maxOutputTokens':
          comparison = (a.maxOutputTokens || 0) - (b.maxOutputTokens || 0);
          break;
        case 'quota':
          comparison = (a.quotaInfo?.remainingFraction ?? -1) - (b.quotaInfo?.remainingFraction ?? -1);
          break;
        case 'images':
          comparison = (a.supportsImages ? 1 : 0) - (b.supportsImages ? 1 : 0);
          break;
        case 'thinking':
          comparison = (a.supportsThinking ? 1 : 0) - (b.supportsThinking ? 1 : 0);
          break;
        case 'video':
          comparison = (a.supportsVideo ? 1 : 0) - (b.supportsVideo ? 1 : 0);
          break;
        case 'usages':
          comparison = (a.usages?.length || 0) - (b.usages?.length || 0);
          break;
      }

      return sortDirection === 'asc' ? comparison : -comparison;
    });

    return sorted;
  }, [modelEntries, sortColumn, sortDirection]);

  const handleSort = (column: SortColumn) => {
    if (sortColumn === column) {
      setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc');
    } else {
      setSortColumn(column);
      setSortDirection('asc');
    }
  };

  const formatQuota = (quota?: QuotaInfo) => {
    if (!quota) return 'N/A';

    const percentage = (quota.remainingFraction * 100).toFixed(1);
    if (!quota.resetTime) {
      return `${percentage}%`;
    }

    const resetDate = new Date(quota.resetTime);
    const now = new Date();
    const diffMs = resetDate.getTime() - now.getTime();

    if (diffMs < 0) {
      return `${percentage}% (reset overdue)`;
    }

    const diffMinutes = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMinutes / 60);
    const diffDays = Math.floor(diffHours / 24);

    let timeStr = '';
    if (diffDays > 0) {
      timeStr = `${diffDays}d ${diffHours % 24}h`;
    } else if (diffHours > 0) {
      timeStr = `${diffHours}h ${diffMinutes % 60}m`;
    } else {
      timeStr = `${diffMinutes}m`;
    }

    return `${percentage}% (resets after ${timeStr})`;
  };

  const getSortIndicator = (column: SortColumn) => {
    if (sortColumn !== column) return null;
    return sortDirection === 'asc' ? ' ↑' : ' ↓';
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content account-details-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <h2>Account Details: {accountEmail}</h2>
          {isQuotaOnly && (
            <p className="quota-only-notice">
              ⚠️ Limited data available (quota-only mode). Some model information is not provided by this account type.
            </p>
          )}
          <button className="close-button" onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>

        <div className="modal-body">
          {modelEntries.length === 0 ? (
            <p className="no-models-message">No models available for this account.</p>
          ) : (
            <div className="table-wrapper">
              <table className="models-table">
                <thead>
                  <tr>
                    <th onClick={() => handleSort('modelId')}>Model ID{getSortIndicator('modelId')}</th>
                    {!isQuotaOnly && (
                      <>
                        <th onClick={() => handleSort('displayName')}>Display Name{getSortIndicator('displayName')}</th>
                        <th onClick={() => handleSort('maxTokens')}>Max Tokens{getSortIndicator('maxTokens')}</th>
                        <th onClick={() => handleSort('maxOutputTokens')}>
                          Max Output{getSortIndicator('maxOutputTokens')}
                        </th>
                        <th onClick={() => handleSort('images')}>Images{getSortIndicator('images')}</th>
                        <th onClick={() => handleSort('video')}>Video{getSortIndicator('video')}</th>
                        <th onClick={() => handleSort('thinking')}>Thinking{getSortIndicator('thinking')}</th>
                        <th onClick={() => handleSort('usages')}>Usages{getSortIndicator('usages')}</th>
                      </>
                    )}
                    <th onClick={() => handleSort('quota')}>Quota Remaining{getSortIndicator('quota')}</th>
                  </tr>
                </thead>
                <tbody>
                  {sortedModels.map((model) => (
                    <tr key={model.modelId} className={model.recommended ? 'recommended' : ''}>
                      <td className="model-id" title={model.modelId}>
                        {model.modelId}
                      </td>
                      {!isQuotaOnly && (
                        <>
                          <td>{model.displayName || model.modelId}</td>
                          <td className="number-cell">{model.maxTokens ? model.maxTokens.toLocaleString() : 'N/A'}</td>
                          <td className="number-cell">
                            {model.maxOutputTokens ? model.maxOutputTokens.toLocaleString() : 'N/A'}
                          </td>
                          <td className="center-cell">{model.supportsImages ? '✓' : '–'}</td>
                          <td className="center-cell">{model.supportsThinking ? '✓' : '–'}</td>
                          <td className="center-cell">{model.supportsVideo ? '✓' : '–'}</td>
                          <td className="usages-cell">
                            {model.usages && model.usages.length > 0 ? model.usages.join(', ') : '–'}
                          </td>
                        </>
                      )}
                      <td className="quota-cell">{formatQuota(model.quotaInfo)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
