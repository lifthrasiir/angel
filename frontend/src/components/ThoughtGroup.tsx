import React, { useState, useEffect } from 'react';
import ChatMessage from './ChatMessage';

interface ThoughtGroupProps {
  thoughts: any[]; // Array of thought messages
  groupId: string; // Unique ID for this thought group
  isAutoDisplayMode: boolean;
  lastAutoDisplayedThoughtId: string | null;
}

export const ThoughtGroup: React.FC<ThoughtGroupProps> = React.memo(({ thoughts, groupId, isAutoDisplayMode, lastAutoDisplayedThoughtId }) => {
  const [activeThoughtIndex, setActiveThoughtIndex] = useState<number | null>(null);
  const [hasBeenManuallySelected, setHasBeenManuallySelected] = useState(false);

  useEffect(() => {
    if (isAutoDisplayMode && !hasBeenManuallySelected) {
      const autoDisplayIndex = thoughts.findIndex(thought => thought.id === lastAutoDisplayedThoughtId);
      setActiveThoughtIndex(autoDisplayIndex !== -1 ? autoDisplayIndex : null);
    } else if (!isAutoDisplayMode && !hasBeenManuallySelected) {
      setActiveThoughtIndex(null);
    }
  }, [isAutoDisplayMode, lastAutoDisplayedThoughtId, thoughts, groupId, hasBeenManuallySelected]);

  const handleCircleClick = (index: number) => {
    setHasBeenManuallySelected(true); // Mark this group as manually selected
    if (activeThoughtIndex === index) {
      // If the same circle is clicked, hide the thought
      setActiveThoughtIndex(null);
    } else {
      // Display the clicked thought
      setActiveThoughtIndex(index);
    }
  };

  return (
    <div style={{ marginBottom: '10px' }}>
      <div style={{ display: 'flex', gap: '5px', marginBottom: '5px', paddingLeft: '15px' }}>
        {thoughts.map((thought, index) => (
          <div
            key={thought.id}
            className={`thought-circle ${activeThoughtIndex === index ? 'selected' : ''}`}
            onClick={() => handleCircleClick(index)}
          ></div>
        ))}
      </div>
      {activeThoughtIndex !== null && (
        <ChatMessage
          key={thoughts[activeThoughtIndex].id}
          role={thoughts[activeThoughtIndex].role}
          text={thoughts[activeThoughtIndex].parts[0].text}
          type={thoughts[activeThoughtIndex].type}
          functionCall={thoughts[activeThoughtIndex].parts[0].functionCall}
          functionResponse={thoughts[activeThoughtIndex].parts[0].functionResponse}
        />
      )}
    </div>
  );
});
