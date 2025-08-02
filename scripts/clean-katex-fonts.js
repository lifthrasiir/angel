const fs = require('node:fs');
const path = require('node:path');

const assetsDir = path.join(__dirname, '..', 'frontend', 'dist', 'assets');

fs.readdir(assetsDir, (err, files) => {
  if (err) {
    console.error('Error reading assets directory:', err);
    return;
  }

  let removedCount = 0;
  files.forEach((file) => {
    if (file.startsWith('KaTeX_') && !file.endsWith('.woff2')) {
      const filePath = path.join(assetsDir, file);
      fs.unlink(filePath, (err) => {
        if (err) {
          console.error(`Error deleting file ${filePath}:`, err);
        } else {
          ++removedCount;
        }
      });
    }
  });
  console.log(`Deleted ${removedCount} file(s).`);
});
