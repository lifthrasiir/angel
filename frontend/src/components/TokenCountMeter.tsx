import React from 'react';
import { useAtom } from 'jotai';
import { messagesAtom, selectedModelAtom } from '../atoms/chatAtoms';

const TokenCountMeter: React.FC = () => {
  const [messages] = useAtom(messagesAtom);
  const [selectedModel] = useAtom(selectedModelAtom);

  const lastMessageTokenCount = messages.length > 0 ? messages[messages.length - 1].cumulTokenCount || 0 : 0;
  const maxTokens = selectedModel?.maxTokens || 0;

  if (maxTokens === 0) {
    return null; // Prevent division by zero
  }

  const usedPercentage = (lastMessageTokenCount / maxTokens) * 100;

  return (
    <div
      style={{
        flex: '0 0 3px',
        width: '100%',
        height: '3px',
        display: 'flex',
        overflow: 'hidden',
        borderRadius: '1.5px', // Half of height for rounded ends
        backgroundColor: '#006400', // Dark green as background
      }}
    >
      <div
        style={{
          width: `${usedPercentage}%`,
          backgroundColor: 'red',
          height: '100%',
          transition: 'width 0.3s ease-in-out', // Add transition for animation
        }}
      />
    </div>
  );
};

export default TokenCountMeter;
