import React from 'react';

interface PendingConfirmationData {
  tool: string;
  file_path?: string;
  content?: string;
  // Add other potential tool arguments here as needed
}

// 새로 추가할 인터페이스
interface ConfirmationDialogProps {
  onConfirm: (modifiedData?: Record<string, any>) => void;
  onDeny: () => void;
  confirmationData: PendingConfirmationData;
}

// 기존 const ConfirmationDialog: React.FC = () => { 부분을 변경
const ConfirmationDialog: React.FC<ConfirmationDialogProps> = ({ onConfirm, onDeny, confirmationData }) => {
  // parsedData 대신 confirmationData prop을 직접 사용
  const parsedData = confirmationData;

  if (!parsedData) return null; // confirmationData가 없으면 렌더링하지 않음

  const actionDescription = parsedData.file_path
    ? `The agent wants to change the file: ${parsedData.file_path}.`
    : `The agent wants to execute the tool: ${parsedData.tool}.`;

  return (
    <div
      style={{
        backgroundColor: '#FFFACD',
        padding: '20px',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        gap: '20px',
      }}
    >
      <p style={{ margin: '0' }}>{actionDescription} What should I do?</p>
      <div style={{ display: 'flex', gap: '10px' }}>
        <button onClick={() => onConfirm()}>1. Approve</button>
        <button onClick={() => onDeny()}>2. Deny</button>
      </div>
    </div>
  );
};

export default ConfirmationDialog;
