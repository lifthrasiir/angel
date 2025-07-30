const fs = require('fs');
const path = require('path');

const exePath = process.platform === 'win32' ? 'angel.exe' : 'angel';
const zipPath = 'frontend.zip';

const fullExePath = path.join(__dirname, '..', exePath);
const fullZipPath = path.join(__dirname, '..', zipPath);

try {
  const exeBuffer = fs.readFileSync(fullExePath);
  const zipBuffer = fs.readFileSync(fullZipPath);

  fs.writeFileSync(fullExePath, Buffer.concat([exeBuffer, zipBuffer]));
  console.log(`Successfully appended ${zipPath} to ${exePath}`);
} catch (error) {
  console.error(`Error during zip append: ${error.message}`);
  process.exit(1);
}
