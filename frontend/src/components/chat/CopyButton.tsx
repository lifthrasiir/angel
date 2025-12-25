import React from 'react';
import { FaCopy } from 'react-icons/fa';
import { useCopyToClipboard } from '../../hooks/useCopyToClipboard';
import './CopyButton.css';

interface CopyButtonProps {
  text: string;
  isDisabled?: boolean;
}

const CopyButton: React.FC<CopyButtonProps> = ({ text, isDisabled = false }) => {
  const { copyToClipboard } = useCopyToClipboard();

  const handleClick = async () => {
    if (isDisabled) return;
    await copyToClipboard(text);
  };

  return (
    <button className="copy-button" onClick={handleClick} disabled={isDisabled} title="Copy message content">
      <FaCopy size={14} />
    </button>
  );
};

export default CopyButton;
