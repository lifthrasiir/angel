import React from 'react';
import { EnvChanged, RootContents, RootAdded, RootRemoved, RootPrompt } from '../types/chat';

interface EnvChangedMessageProps {
  envChanged: EnvChanged;
  messageId?: string;
}

const RootContentsDisplay: React.FC<{ contents: RootContents[]; indent: string }> = ({ contents, indent }) => {
  return (
    <ul style={{ listStyle: 'none', padding: 0, margin: 0 }}>
      {contents.map((content, index) => (
        <li key={index} style={{ marginBottom: '4px' }}>
          <span style={{ fontFamily: 'monospace', fontSize: '0.875rem' }}>
            {indent}
            {content.path}
            {content.isDir ? '/' : ''}
            {content.hasMore ? ' ...' : ''}
          </span>
          {content.children && content.children.length > 0 && (
            <RootContentsDisplay contents={content.children} indent={indent + '  '} />
          )}
        </li>
      ))}
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
    <div id={messageId} style={{ padding: '8px', border: '1px solid #ccc', borderRadius: '4px', marginBottom: '8px' }}>
      <div style={{ background: '#f0f0f0', padding: '12px', borderRadius: '4px' }}>
        <p style={{ fontWeight: 'bold', marginBottom: '8px' }}>Working environment has changed:</p>

        {roots.added && roots.added.length > 0 && (
          <div style={{ marginBottom: '16px' }}>
            <h3 style={{ fontSize: '1.125rem', fontWeight: 'bold', marginBottom: '4px' }}>Added directories:</h3>
            {roots.added.map((addedRoot: RootAdded, index: number) => (
              <div key={index} style={{ marginBottom: '8px' }}>
                <p style={{ fontFamily: 'monospace', fontSize: '0.875rem', fontWeight: 'bold' }}># {addedRoot.path}</p>
                <RootContentsDisplay contents={addedRoot.contents ?? []} indent="  " />
              </div>
            ))}
          </div>
        )}

        {roots.removed && roots.removed.length > 0 && (
          <div style={{ marginBottom: '16px' }}>
            <h3 style={{ fontSize: '1.125rem', fontWeight: 'bold', marginBottom: '4px' }}>Removed directories:</h3>
            <p style={{ fontSize: '0.875rem', fontStyle: 'italic', marginBottom: '4px' }}>Paths no longer available:</p>
            <ul style={{ listStyle: 'disc', marginLeft: '20px', padding: 0 }}>
              {roots.removed.map((removedRoot: RootRemoved, index: number) => (
                <li key={index} style={{ fontFamily: 'monospace', fontSize: '0.875rem' }}>
                  - {removedRoot.path}
                </li>
              ))}
            </ul>
          </div>
        )}

        <hr style={{ margin: '16px 0', borderTop: '1px solid #ccc' }} />

        <p style={{ fontWeight: 'bold', marginBottom: '8px' }}>Per-directory directives:</p>
        {roots.removed && roots.removed.length > 0 && (
          <p style={{ fontSize: '0.875rem', fontStyle: 'italic', marginBottom: '8px' }}>
            Forget all prior per-directory directives in advance.
          </p>
        )}

        {roots.prompts && roots.prompts.length > 0 && (
          <div>
            {roots.prompts.map((prompt: RootPrompt, index: number) => (
              <div key={index} style={{ marginBottom: '8px' }}>
                r<p style={{ fontFamily: 'monospace', fontSize: '0.875rem', fontWeight: 'bold' }}># {prompt.path}</p>
                <pre
                  style={{
                    background: '#e0e0e0',
                    padding: '8px',
                    borderRadius: '4px',
                    fontSize: '0.875rem',
                    overflow: 'auto',
                  }}
                >
                  {prompt.prompt}
                </pre>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default EnvChangedMessage;
