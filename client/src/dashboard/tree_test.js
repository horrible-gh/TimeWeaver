const fs = require('fs');
const path = require('path');

function printDirectoryStructure(dirPath, indent = '') {
  const items = fs.readdirSync(dirPath);

  items.forEach(item => {
    const fullPath = path.join(dirPath, item);
    const stats = fs.statSync(fullPath);

    if (stats.isDirectory()) {
      console.log(`${indent}📁 ${item}/`);
      printDirectoryStructure(fullPath, indent + '  ');
    } else {
      console.log(`${indent}📄 ${item}`);
    }
  });
}

// Usage example
const startPath = './'; // Based on the current directory
printDirectoryStructure(startPath);
