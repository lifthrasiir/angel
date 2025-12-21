import React, { useEffect, useState } from 'react';
import { useProcessingState } from '../hooks/useProcessingState';

interface ProcessingIndicatorProps {
  isLastThoughtGroup: boolean;
  isLastModelMessage: boolean;
}

const formatTime = (milliseconds: number): string => {
  const totalSeconds = Math.floor(milliseconds / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  let formattedTime = '';
  if (hours > 0) {
    formattedTime += `${hours}h `;
  }
  if (minutes > 0 || hours > 0) {
    // Show minutes if hours are present or minutes are non-zero
    formattedTime += `${minutes}m `;
  }
  formattedTime += `${seconds}s`;
  return formattedTime.trim();
};

export const ProcessingIndicator: React.FC<ProcessingIndicatorProps> = ({ isLastThoughtGroup, isLastModelMessage }) => {
  const { startTime } = useProcessingState();
  const [_, setForceUpdate] = useState(0); // Dummy state to force re-render

  useEffect(() => {
    if (!startTime) return;

    const interval = setInterval(() => {
      setForceUpdate((prev) => prev + 1); // Force re-render every 100ms
    }, 100); // Check every 100ms for more accurate updates

    return () => clearInterval(interval);
  }, [startTime]); // Still depends on startTime to reset interval if processing starts/stops

  // Don't render if there's no startTime (not processing)
  if (!startTime) {
    return null;
  }

  const indicatorStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    height: '15px',
    gap: '4px',
    color: '#666',
    fontSize: '0.9em',
    marginTop: isLastThoughtGroup || isLastModelMessage ? '0' : '10px', // Adjust margin based on position
    marginBottom: isLastThoughtGroup ? '0' : '10px',
  };

  const logoStyle: React.CSSProperties = {
    width: '20px', // Reduced size
    height: '20px',
    transform: 'scaleX(-1)', // Flip horizontally
    animation: 'blink-animation 0.8s infinite alternate',
  };

  const currentElapsedTime = performance.now() - startTime;

  return (
    <div style={indicatorStyle}>
      <style>{`
        @keyframes blink-animation {
          from { opacity: 1; }
          to { opacity: 0.3; }
        }
      `}</style>
      <img src="/angel-logo-colored.svg" alt="Processing" style={logoStyle} />
      <span>{formatTime(currentElapsedTime)}</span>
    </div>
  );
};
