import type React from 'react';
import './Tooltip.css';

interface TooltipProps {
  content: string;
  children: React.ReactNode;
  position?: 'top' | 'bottom' | 'left' | 'right';
  delay?: number;
}

const Tooltip: React.FC<TooltipProps> = ({ content, children, position = 'bottom', delay = 0 }) => {
  return (
    <span className="tooltip-container" style={{ '--delay': `${delay}ms` } as React.CSSProperties} title="">
      {children}
      <span className={`tooltip tooltip-${position}`}>{content}</span>
    </span>
  );
};

export default Tooltip;
