import React from 'react';

interface BlobImageProps {
  sessionId: string;
  hash: string;
  alt?: string;
  className?: string;
  style?: React.CSSProperties;
}

const BlobImage: React.FC<BlobImageProps> = ({
  sessionId,
  hash,
  alt = `Image ${hash.substring(0, 8)}`,
  className = 'image-only-message-img',
  style = {
    display: 'inline-block',
    verticalAlign: 'top',
  },
}) => {
  return <img src={`/${sessionId}/@${hash}`} alt={alt} className={className} style={style} />;
};

export default BlobImage;
