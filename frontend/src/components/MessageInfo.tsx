import React from 'react';
import type { PossibleNextMessage } from '../types/chat';

interface MessageInfoProps {
  cumulTokenCount?: number | null;
  branchId?: string;
  parentMessageId?: string;
  chosenNextId?: string;
  possibleNextIds?: PossibleNextMessage[];
  model?: string; // New prop for the model that generated the message
}

const MessageInfo: React.FC<MessageInfoProps> = ({
  cumulTokenCount,
  branchId,
  parentMessageId,
  chosenNextId,
  possibleNextIds,
  model,
}) => {
  const hasInfo =
    cumulTokenCount !== undefined ||
    branchId ||
    parentMessageId ||
    chosenNextId ||
    (possibleNextIds && possibleNextIds.length > 0) ||
    model;

  if (!hasInfo) {
    return null;
  }

  return (
    <details className="message-info-details">
      <summary className="message-info-summary">Message Info</summary>
      <div className="message-info-content">
        {model && <div className="model-name">Model: {model}</div>}
        {cumulTokenCount !== undefined && cumulTokenCount !== null && (
          <div className="token-count">Tokens: {cumulTokenCount}</div>
        )}
        {branchId && <div className="branch-id">Branch ID: {branchId}</div>}
        {parentMessageId && <div className="parent-message-id">Parent Message ID: {parentMessageId}</div>}
        {chosenNextId && <div className="chosen-next-id">Chosen Next ID: {chosenNextId}</div>}
        {possibleNextIds && possibleNextIds.length > 0 && (
          <div className="possible-next-ids">
            Possible Next IDs:
            <ul>
              {possibleNextIds.map((item) => (
                <li key={item.messageId}>
                  {item.messageId} (Branch: {item.branchId})
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </details>
  );
};

export default MessageInfo;
