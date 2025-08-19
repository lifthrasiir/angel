import React, { useEffect, useState } from 'react';
import { useAtom } from 'jotai';

import { lastAutoDisplayedThoughtIdAtom } from '../atoms/chatAtoms';
import type { ChatMessage } from '../types/chat';
import ChatMessageComponent from './ChatMessage';
import { ProcessingIndicator } from './ProcessingIndicator';

interface ThoughtGroupProps {
  groupId: string; // Unique ID for this thought group
  isAutoDisplayMode: boolean;
  thoughts: ChatMessage[];
  isLastThoughtGroup?: boolean;
  processingStartTime?: number | null;
}

export const ThoughtGroup: React.FC<ThoughtGroupProps> = React.memo(
  ({ groupId, isAutoDisplayMode, thoughts, isLastThoughtGroup, processingStartTime }) => {
    const [lastAutoDisplayedThoughtId] = useAtom(lastAutoDisplayedThoughtIdAtom);

    const [activeThoughtId, setActiveThoughtId] = useState<string | null>(null);
    const [hasBeenManuallySelected, setHasBeenManuallySelected] = useState(false);

    useEffect(() => {
      if (isAutoDisplayMode && !hasBeenManuallySelected) {
        const autoDisplayThought = thoughts.find((thought) => thought.id === lastAutoDisplayedThoughtId);
        setActiveThoughtId(autoDisplayThought ? autoDisplayThought.id : null);
      } else if (!isAutoDisplayMode && !hasBeenManuallySelected) {
        setActiveThoughtId(null);
      }
    }, [isAutoDisplayMode, lastAutoDisplayedThoughtId, thoughts, groupId, hasBeenManuallySelected]);

    const handleCircleClick = (thought: ChatMessage) => {
      setHasBeenManuallySelected(true);
      if (activeThoughtId === thought.id) {
        setActiveThoughtId(null);
      } else {
        setActiveThoughtId(thought.id);
      }
    };

    const getThoughtTitle = (thought: ChatMessage) => {
      if (!thought.parts || thought.parts.length === 0 || !thought.parts[0].text) return '';
      const lines = thought.parts[0].text.split('\n');
      const title = lines[0].trim();
      const content = lines.slice(1).join('\n').trim();
      return `${title}\n\n${content}`;
    };

    return (
      <div className="thought-group-container">
        <div className="thought-circle-container">
          {thoughts.map((thought) => (
            <button
              key={thought.id}
              className={`thought-circle ${activeThoughtId === thought.id ? 'selected' : ''}`}
              onClick={() => handleCircleClick(thought)}
              title={getThoughtTitle(thought)}
              aria-label={`Thought: ${getThoughtTitle(thought).split('\n')[0]}`}
            ></button>
          ))}
          {isLastThoughtGroup && processingStartTime !== null && (
            <ProcessingIndicator
              startTime={processingStartTime!}
              isLastThoughtGroup={true}
              isLastModelMessage={false}
            />
          )}
        </div>
        {activeThoughtId !== null && thoughts.find((thought) => thought.id === activeThoughtId) && (
          <ChatMessageComponent
            key={activeThoughtId}
            message={thoughts.find((thought) => thought.id === activeThoughtId)!}
          />
        )}
      </div>
    );
  },
);
