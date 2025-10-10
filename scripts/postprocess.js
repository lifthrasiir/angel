const fs = require('node:fs/promises');
const path = require('node:path');

const frontendDistDir = path.join(__dirname, '..', 'frontend', 'dist');
const sourcemapsDir = path.join(__dirname, '..', 'frontend', 'sourcemaps');
const assetsDir = path.join(frontendDistDir, 'assets');

async function main() {
  try {
    // Create sourcemaps directory if it doesn't exist
    await fs.mkdir(sourcemapsDir, { recursive: true });

    console.log('Starting post-build cleanup...');

    // Read assets directory
    const files = await fs.readdir(assetsDir);

    let removedCount = 0;
    let movedCount = 0;
    let updatedCount = 0;

    const operations = files.map(async (file) => {
      const filePath = path.join(assetsDir, file);

      // Remove KaTeX font files (except .woff2)
      if (file.startsWith('KaTeX_') && !file.endsWith('.woff2')) {
        try {
          await fs.unlink(filePath);
          removedCount++;
        } catch (err) {
          console.error(`Error deleting file ${filePath}:`, err);
        }
      }
      // Move sourcemap files to sourcemaps directory
      else if (file.endsWith('.map')) {
        try {
          const targetPath = path.join(sourcemapsDir, file);
          await fs.rename(filePath, targetPath);
          movedCount++;
          console.log(`Moved sourcemap: ${file}`);
        } catch (err) {
          console.error(`Error moving file ${file}:`, err);
        }
      }
      // Update sourceMappingURL in JavaScript files
      else if (file.endsWith('.js')) {
        try {
          const content = await fs.readFile(filePath, 'utf8');

          // Replace sourceMappingURL to point to the new location
          const updatedContent = content.replace(
            /\/\/# sourceMappingURL=(.+\.map)/g,
            '//# sourceMappingURL=../sourcemaps/$1'
          );

          if (content !== updatedContent) {
            await fs.writeFile(filePath, updatedContent, 'utf8');
            updatedCount++;
            console.log(`Updated sourceMappingURL in: ${file}`);
          }
        } catch (err) {
          console.error(`Error processing file ${file}:`, err);
        }
      }
    });

    // Wait for all operations to complete
    await Promise.all(operations);

    console.log(`Deleted ${removedCount} KaTeX font file(s).`);
    console.log(`Moved ${movedCount} sourcemap file(s) to sourcemaps directory.`);
    console.log(`Updated ${updatedCount} sourceMappingURL reference(s).`);
    console.log('Post-build cleanup completed.');
  } catch (err) {
    console.error('Error during post-build cleanup:', err);
    process.exit(1);
  }
}

main();