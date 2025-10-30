/**
 * Image resizing utilities for attachment optimization
 */

export interface ImageDimensions {
  width: number;
  height: number;
}

export interface ResizeOptions {
  quality?: number; // 0.0 to 1.0
}

export interface ImageResizeResult {
  blob: Blob;
  dimensions: ImageDimensions;
  originalSize: number;
  compressedSize: number;
  compressionRatio: number;
}

// Tile size for image processing
export const TILE_SIZE = 768;
export const MAX_TILES = 4;

// Default options for image resizing
export const DEFAULT_RESIZE_OPTIONS: ResizeOptions = {
  quality: 0.9,
};

/**
 * Calculate the optimal scale factor to limit image tiles to MAX_TILES
 * We need: ceil(width × scale / TILE_SIZE) × ceil(height × scale / TILE_SIZE) ≤ MAX_TILES
 * This function directly calculates tile counts and adjusts the scale factor if needed
 */
export const calculateTileScaleFactor = (dimensions: ImageDimensions): number => {
  let { width, height } = dimensions;

  // Ensure w ≥ h for the calculation
  if (height > width) {
    [width, height] = [height, width];
  }

  // Case 1: 2×2 grid constraint
  // ceil(w×scale/TILE_SIZE) ≤ 2 ⇒ w×scale/TILE_SIZE ≤ 2 ⇒ scale ≤ (2×TILE_SIZE)/w
  // ceil(h×scale/TILE_SIZE) ≤ 2 ⇒ h×scale/TILE_SIZE ≤ 2 ⇒ scale ≤ (2×TILE_SIZE)/h
  const case1Scale = Math.min((2 * TILE_SIZE) / width, (2 * TILE_SIZE) / height);

  // Case 2: 1×4 grid constraint
  // ceil(w×scale/TILE_SIZE) ≤ 4 ⇒ w×scale/TILE_SIZE ≤ 4 ⇒ scale ≤ (4×TILE_SIZE)/w
  // ceil(h×scale/TILE_SIZE) = 1 ⇒ h×scale/TILE_SIZE ≤ 1 ⇒ scale ≤ (1×TILE_SIZE)/h
  const case2Scale = Math.min((4 * TILE_SIZE) / width, (1 * TILE_SIZE) / height);

  // Choose the larger scale factor that satisfies the constraints
  let optimalScale = Math.max(case1Scale, case2Scale);

  // Ensure we don't scale up (factor > 1 means enlargement)
  optimalScale = Math.min(optimalScale, 1.0);

  // Safety check: verify the scale factor actually produces valid tile counts
  // Calculate actual tile counts with this scale factor
  const scaledWidth = width * optimalScale;
  const scaledHeight = height * optimalScale;
  const tilesX = Math.ceil(scaledWidth / TILE_SIZE);
  const tilesY = Math.ceil(scaledHeight / TILE_SIZE);
  const totalTiles = tilesX * tilesY;

  // Safety check: if we exceed the limit due to floating-point precision,
  // reduce scale factor slightly using nextafter semantics
  if (totalTiles > MAX_TILES) {
    // Multiply by a value very close to 1 (but slightly less) to get nextafter effect
    // This is essentially nextafter(optimalScale, 0) for normal numbers
    optimalScale *= 0.9999999999999999; // 16 9's after decimal
  }

  return optimalScale;
};

/**
 * Check if an image should be resized based on tile constraints
 */
export const shouldResizeImage = (dimensions: ImageDimensions): boolean => {
  const scaleFactor = calculateTileScaleFactor(dimensions);
  return scaleFactor < 1.0;
};

/**
 * Calculate new dimensions based on tile constraints
 */
export const calculateTileConstrainedDimensions = (dimensions: ImageDimensions): ImageDimensions => {
  const scaleFactor = calculateTileScaleFactor(dimensions);

  // If scale factor is 1.0, no resizing needed
  if (scaleFactor >= 1.0) {
    return dimensions;
  }

  return {
    width: Math.floor(dimensions.width * scaleFactor),
    height: Math.floor(dimensions.height * scaleFactor),
  };
};

/**
 * Get image dimensions from a File object
 */
export const getImageDimensions = (file: File): Promise<ImageDimensions> => {
  return new Promise((resolve, reject) => {
    if (!file.type.startsWith('image/')) {
      reject(new Error('File is not an image'));
      return;
    }

    const img = new Image();
    img.onload = () => {
      resolve({
        width: img.naturalWidth,
        height: img.naturalHeight,
      });
    };
    img.onerror = reject;
    img.src = URL.createObjectURL(file);
  });
};

/**
 * Resize an image file using canvas
 */
export const resizeImage = (
  file: File,
  options: ResizeOptions = DEFAULT_RESIZE_OPTIONS,
): Promise<ImageResizeResult> => {
  return new Promise((resolve, reject) => {
    if (!file.type.startsWith('image/')) {
      reject(new Error('File is not an image'));
      return;
    }

    const { quality = DEFAULT_RESIZE_OPTIONS.quality! } = options;

    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');

      if (!ctx) {
        reject(new Error('Could not get canvas context'));
        return;
      }

      // Get original dimensions and calculate tile-constrained dimensions
      const originalDimensions = {
        width: img.naturalWidth,
        height: img.naturalHeight,
      };

      let { width, height } = calculateTileConstrainedDimensions(originalDimensions);

      canvas.width = width;
      canvas.height = height;

      // Draw and resize image
      ctx.drawImage(img, 0, 0, width, height);

      // Convert to blob
      canvas.toBlob(
        (blob) => {
          if (!blob) {
            reject(new Error('Could not create blob from canvas'));
            return;
          }

          const originalSize = file.size;
          const compressedSize = blob.size;
          const compressionRatio = originalSize > 0 ? compressedSize / originalSize : 1;

          resolve({
            blob,
            dimensions: { width, height },
            originalSize,
            compressedSize,
            compressionRatio,
          });
        },
        file.type,
        quality,
      );
    };

    img.onerror = reject;
    img.src = URL.createObjectURL(file);
  });
};

/**
 * Convert a resized blob back to a File object
 */
export const resizedBlobToFile = (blob: Blob, originalFile: File, suffix: string = '_resized'): File => {
  const nameParts = originalFile.name.split('.');
  const extension = nameParts.pop();
  const baseName = nameParts.join('.');
  const newName = extension ? `${baseName}${suffix}.${extension}` : `${baseName}${suffix}`;

  return new File([blob], newName, { type: originalFile.type });
};

/**
 * Format file size for display
 */
export const formatFileSize = (bytes: number): string => {
  if (bytes === 0) return '0 Bytes';

  const k = 1024;
  const sizes = ['Bytes', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
};

/**
 * Format dimensions for display
 */
export const formatDimensions = (dimensions: ImageDimensions): string => {
  return `${dimensions.width}×${dimensions.height}`;
};
