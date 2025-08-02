import { renderToString } from 'react-dom/server';
import PrettyJSON from '../components/PrettyJSON';
import React from 'react';

export const measureContentHeight = (
  messageRef: React.RefObject<HTMLDivElement>,
  showPrettyJson: boolean,
  codeContent: string,
  data: any, // functionCall.args or functionResponse.response
  soleObjectKey?: string, // for functionResponse
): number => {
  if (!messageRef.current) {
    return 0;
  }

  const tempDiv = document.createElement('div');
  if (showPrettyJson) {
    tempDiv.innerHTML = renderToString(
      React.createElement(PrettyJSON, { data: soleObjectKey ? data[soleObjectKey] : data }),
    );
  } else {
    const preElement = document.createElement('pre');
    preElement.className = 'function-code-block';
    preElement.textContent = codeContent;
    tempDiv.appendChild(preElement);
  }

  tempDiv.style.position = 'absolute';
  tempDiv.style.visibility = 'hidden';
  tempDiv.style.height = 'auto';
  tempDiv.style.maxHeight = 'none';
  tempDiv.style.width = messageRef.current.clientWidth + 'px';
  document.body.appendChild(tempDiv);

  const contentHeight = tempDiv.scrollHeight;
  document.body.removeChild(tempDiv);

  return contentHeight;
};
