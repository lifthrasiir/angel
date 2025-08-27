import React from 'react';
import { EnvChanged, RootContents, RootAdded, RootRemoved, RootPrompt } from '../types/chat';
import MarkdownRenderer from './MarkdownRenderer'; // Import MarkdownRenderer

interface EnvChangedMessageProps {
  envChanged: EnvChanged;
  messageId?: string;
}

const RootContentsDisplay: React.FC<{ contents: RootContents[]; indent: string }> = ({ contents, indent }) => {
  return (
    <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
      {contents.map((content, index) =>
        typeof content === 'string' ? (
          <li key={index} style={{ margin: 0 }}>
            {indent}
            {content.endsWith('/') ? <strong>{content}</strong> : content}
          </li>
        ) : (
          <li key={index} style={{ margin: 0 }}>
            {indent}
            <strong>{content.name}</strong>
            {content.children.length > 0 && <RootContentsDisplay contents={content.children} indent={indent + '  '} />}
          </li>
        ),
      )}
    </ul>
  );
};

const EnvChangedMessage: React.FC<EnvChangedMessageProps> = ({ envChanged, messageId }) => {
  const { roots } = envChanged;

  if (!roots) {
    console.warn('Nothing found in EnvChanged message:', envChanged);
    return null; // Should not happen if backend sends valid EnvChanged
  }

  return (
    <div id={messageId} className="chat-message-container system-message">
      <div className="chat-bubble">
        <p>
          <strong>Working environment has changed:</strong>
        </p>

        {roots.added &&
          roots.added.length > 0 &&
          roots.added.map((addedRoot: RootAdded, index: number) => (
            <details key={index} style={{ marginBottom: '8px' }}>
              <summary style={{ cursor: 'pointer' }}>
                Added directory: <code>{addedRoot.path}</code>
              </summary>
              {addedRoot.contents && addedRoot.contents.length > 0 ? (
                <pre>
                  <RootContentsDisplay contents={addedRoot.contents} indent="" />
                </pre>
              ) : (
                <p>This directory is empty.</p>
              )}
            </details>
          ))}

        {roots.removed && roots.removed.length > 0 && (
          <details style={{ marginBottom: '16px' }}>
            <summary style={{ cursor: 'pointer' }}>Removed directories</summary>
            <ul style={{ listStyle: 'none', marginLeft: '20px', padding: 0 }}>
              {roots.removed.map((removedRoot: RootRemoved, index: number) => (
                <li key={index}>
                  - <code>{removedRoot.path}</code>
                </li>
              ))}
            </ul>
          </details>
        )}

        {roots.prompts &&
          roots.prompts.length > 0 &&
          roots.prompts.map((prompt: RootPrompt, index: number) => (
            <details key={index} style={{ marginBottom: '8px' }}>
              <summary style={{ cursor: 'pointer' }}>
                Directives from <code>{prompt.path}</code>
              </summary>
              <div
                style={{
                  backgroundColor: '#ffffff80',
                  border: '1px solid var(--color-system-dark)',
                  padding: '8px',
                  borderRadius: '4px',
                  marginTop: '4px',
                }}
              >
                <MarkdownRenderer content={prompt.prompt} />
              </div>
            </details>
          ))}
      </div>
    </div>
  );
};

export default EnvChangedMessage;
