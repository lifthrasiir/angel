import React, { useMemo } from 'react';
import { useHighlightCode } from '../utils/highlightUtils';
import './PrettyDiff.css';

interface PrettyDiffProps {
  diffContent: string;
  baseLanguage?: string; // Optional: hint for the language of the code within the diff
}

const PrettyDiff: React.FC<PrettyDiffProps> = ({ diffContent, baseLanguage }) => {
  const lines = useMemo(() => diffContent.split(/\r?\n/), [diffContent]);

  return (
    <pre className="pretty-diff-container">
      <code>
        {lines.map((line, index) => {
          const firstChar = line.charAt(0);
          let className = 'pretty-diff-line';
          let contentToHighlight = line;
          let languageToHighlight = baseLanguage;

          let highlightedLineContent: string;
          let renderPrefix = true;

          if (firstChar === '@') {
            className += ' pretty-diff-header';
            languageToHighlight = undefined;
            highlightedLineContent = line;
            renderPrefix = false;
          } else if (firstChar === '\\') {
            className += ' pretty-diff-no-newline';
            languageToHighlight = undefined;
            highlightedLineContent = line;
            renderPrefix = false;
          } else if (firstChar === '+') {
            className += ' pretty-diff-added';
            contentToHighlight = line.substring(1);
            highlightedLineContent = useHighlightCode(contentToHighlight, languageToHighlight);
          } else if (firstChar === '-') {
            className += ' pretty-diff-removed';
            contentToHighlight = line.substring(1);
            highlightedLineContent = useHighlightCode(contentToHighlight, languageToHighlight);
          } else if (firstChar === ' ') {
            className += ' pretty-diff-context';
            contentToHighlight = line.substring(1);
            highlightedLineContent = useHighlightCode(contentToHighlight, languageToHighlight);
          } else {
            // Fallback for other lines (should not happen with unified diff)
            highlightedLineContent = useHighlightCode(contentToHighlight, languageToHighlight);
          }

          return (
            <div key={index} className={className}>
              {renderPrefix && <span className="pretty-diff-line-prefix">{firstChar}</span>}
              <span dangerouslySetInnerHTML={{ __html: highlightedLineContent }} />
            </div>
          );
        })}
      </code>
    </pre>
  );
};

export default PrettyDiff;
