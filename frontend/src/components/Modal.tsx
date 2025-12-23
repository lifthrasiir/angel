import React, { useEffect } from 'react';
import './Modal.css';

export interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  children: React.ReactNode;
  className?: string;
  maxWidth?: string;
  maxHeight?: string;
  maxWidthPercentage?: string;
}

export interface ModalHeaderProps {
  children: React.ReactNode;
  onClose: () => void;
  closeAriaLabel?: string;
}

export interface ModalBodyProps {
  children: React.ReactNode;
}

export interface ModalFooterProps {
  children: React.ReactNode;
}

export const Modal: React.FC<ModalProps> & {
  Header: React.FC<ModalHeaderProps>;
  Body: React.FC<ModalBodyProps>;
  Footer: React.FC<ModalFooterProps>;
} = ({ isOpen, onClose, children, className = '', maxWidth, maxHeight, maxWidthPercentage }) => {
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const contentStyle: React.CSSProperties = {};
  if (maxWidth) contentStyle.maxWidth = maxWidth;
  if (maxHeight) contentStyle.maxHeight = maxHeight;
  if (maxWidthPercentage) contentStyle.width = maxWidthPercentage;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className={`modal-content ${className}`} onClick={(e) => e.stopPropagation()} style={contentStyle}>
        {children}
      </div>
    </div>
  );
};

const ModalHeader: React.FC<ModalHeaderProps> = ({ children, onClose, closeAriaLabel = 'Close' }) => {
  return (
    <div className="modal-header">
      {children}
      <button className="close-button" onClick={onClose} aria-label={closeAriaLabel}>
        ×
      </button>
    </div>
  );
};

const ModalBody: React.FC<ModalBodyProps> = ({ children }) => {
  return <div className="modal-body">{children}</div>;
};

const ModalFooter: React.FC<ModalFooterProps> = ({ children }) => {
  return <div className="modal-footer">{children}</div>;
};

Modal.Header = ModalHeader;
Modal.Body = ModalBody;
Modal.Footer = ModalFooter;
