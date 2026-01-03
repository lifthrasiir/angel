import type React from 'react';

interface TemporarySessionNoticeProps {
  show: boolean;
}

export const TemporarySessionNotice: React.FC<TemporarySessionNoticeProps> = ({ show }) => {
  if (!show) return null;

  return (
    <div
      style={{
        textAlign: 'center',
        padding: '20px',
        margin: '0 0 20px 0',
        backgroundColor: '#fff3cd',
        border: '1px solid #ffeaa7',
        borderRadius: '8px',
        color: '#856404',
        fontSize: '16px',
        fontWeight: '500',
      }}
    >
      This is a temporary session, to be deleted after 48 hours of inactivity.
    </div>
  );
};

export default TemporarySessionNotice;
