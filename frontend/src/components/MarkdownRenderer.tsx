import type React from 'react';
import { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import { useSetAtom } from 'jotai';
import breaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import remarkHtml from 'remark-html';
import 'katex/dist/katex.min.css';

import { smartPreprocessMarkdown } from '../lib/markdown-preprocessor';
import { rehypeHandleRawNodes } from '../lib/rehype/rehype-handle-raw-nodes';
import { highlightJsCommonLoadedAtom } from '../atoms/highlightAtoms';
import { loadHighlightJsCommon } from '../utils/highlightUtils'; // ADD

interface MarkdownRendererProps {
  content: string;
}

const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({ content }) => {
  // KaTeX related states
  const [katexLoaded, setKatexLoaded] = useState(false);
  const [remarkMath, setRemarkMath] = useState<any>(null);
  const [rehypeKatex, setRehypeKatex] = useState<any>(null);

  // Highlight.js related states
  const [highlightLoaded, setHighlightLoaded] = useState(false);
  const [rehypeHighlightModule, setRehypeHighlightModule] = useState<any>(null);
  const setHighlightJsCommonLoaded = useSetAtom(highlightJsCommonLoadedAtom);

  // Effect for KaTeX loading
  useEffect(() => {
    Promise.all([import('remark-math'), import('rehype-katex')])
      .then(([remarkMathModule, rehypeKatexModule]) => {
        setRemarkMath(() => remarkMathModule.default);
        setRehypeKatex(() => rehypeKatexModule.default);
        setKatexLoaded(true);
      })
      .catch((error) => {
        console.error('Failed to load KaTeX modules:', error);
      });
  }, []);

  // Effect for Highlight.js loading
  useEffect(() => {
    Promise.all([
      import('rehype-highlight'),
      loadHighlightJsCommon(setHighlightJsCommonLoaded), // Call loadHighlightJsCommon here
    ])
      .then(([rehypeHighlightDynamic]) => {
        setRehypeHighlightModule(() => rehypeHighlightDynamic.default);
        setHighlightLoaded(true);
      })
      .catch((error) => {
        console.error('Failed to load Highlight.js modules:', error);
      });
  }, []);

  const preprocessedText = smartPreprocessMarkdown(content || '');

  const commonRehypePlugins = [rehypeHandleRawNodes];
  if (highlightLoaded && rehypeHighlightModule) {
    commonRehypePlugins.push(rehypeHighlightModule);
  }
  if (katexLoaded && rehypeKatex) {
    commonRehypePlugins.push(rehypeKatex);
  }

  return (
    <ReactMarkdown
      remarkPlugins={[remarkGfm, breaks, remarkHtml, ...(katexLoaded ? [remarkMath] : [])]}
      rehypePlugins={commonRehypePlugins}
      components={{
        p: ({ node, ...props }) => {
          return <p {...props} />;
        },
      }}
    >
      {preprocessedText}
    </ReactMarkdown>
  );
};

export default MarkdownRenderer;
