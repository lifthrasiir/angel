import React from 'react';
import LogoAnimation from '../components/LogoAnimation';

const NotFoundPage: React.FC = () => {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', justifyContent: 'center', alignItems: 'center', height: '100vh', width: '100%', fontSize: '1.2em' }}>
      <LogoAnimation width="100px" height="100px" color="#007bff" />
      <p style={{ marginTop: '20px' }}>404 Not Found</p>
    </div>
  );
};

export default NotFoundPage;
