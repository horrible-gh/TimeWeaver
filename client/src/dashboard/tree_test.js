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

// 사용 예시
const startPath = './'; // 현재 디렉토리 기준
printDirectoryStructure(startPath);
