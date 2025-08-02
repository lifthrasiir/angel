import type React from 'react';
import { useEffect, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import breaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';
import remarkHtml from 'remark-html';
import 'katex/dist/katex.min.css';
import { smartPreprocessMarkdown } from '../lib/markdown-preprocessor';
import { rehypeHandleRawNodes } from '../lib/rehype/rehype-handle-raw-nodes';

interface MarkdownRendererProps {
  content: string;
}

const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({ content }) => {
  const [katexLoaded, setKatexLoaded] = useState(false);
  const [remarkMath, setRemarkMath] = useState<any>(null);
  const [rehypeKatex, setRehypeKatex] = useState<any>(null);

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

  const preprocessedText = smartPreprocessMarkdown(content || '');

  return (
    <ReactMarkdown
      remarkPlugins={katexLoaded ? [remarkGfm, breaks, remarkHtml, remarkMath] : [remarkGfm, breaks, remarkHtml]}
      rehypePlugins={katexLoaded ? [rehypeHandleRawNodes, rehypeKatex] : [rehypeHandleRawNodes]}
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
