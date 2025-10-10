import React from 'react';

interface BlobImageProps {
  hash: string;
  alt?: string;
  className?: string;
  style?: React.CSSProperties;
}

const BlobImage: React.FC<BlobImageProps> = ({
  hash,
  alt = `Image ${hash.substring(0, 8)}`,
  className = 'image-only-message-img',
  style = {
    display: 'inline-block',
    margin: '5px',
    verticalAlign: 'top',
  },
}) => {
  return <img src={`/api/blob/${hash}`} alt={alt} className={className} style={style} />;
};

export default BlobImage;
