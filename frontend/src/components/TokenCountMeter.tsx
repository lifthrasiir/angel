import React from 'react';
import { useAtom } from 'jotai';
import { messagesAtom } from '../atoms/chatAtoms';
import { selectedModelAtom } from '../atoms/modelAtoms';

const TokenCountMeter: React.FC = () => {
  const [messages] = useAtom(messagesAtom);
  const [selectedModel] = useAtom(selectedModelAtom);

  let latestCumulTokenCount = 0;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].cumulTokenCount) {
      latestCumulTokenCount = messages[i].cumulTokenCount!;
      break;
    }
  }
  const maxTokens = selectedModel?.maxTokens || 0;

  if (maxTokens === 0) {
    return null; // Prevent division by zero
  }

  const usedPercentage = (latestCumulTokenCount / maxTokens) * 100;

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
