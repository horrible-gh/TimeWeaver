import os
import subprocess
import re

PORT = 8000  # ✅ Uvicorn port to stop

# 🔍 Find the running process on port 8000
command = f'netstat -ano | findstr :{PORT}'
result = subprocess.run(command, capture_output=True, text=True, shell=True)

# 🔍 Extract only LISTENING PIDs
pids = set()
for line in result.stdout.splitlines():
    if "LISTENING" in line:
        match = re.search(r'\d+$', line)
        if match:
            pids.add(match.group())

if not pids:
    print("❌ No running Uvicorn process found.")
else:
    for pid in pids:
        print(f"🔍 Uvicorn process PID to stop: {pid}")
        os.system(f"taskkill /PID {pid} /F")
        print(f"✅ PID {pid} stopped successfully!")
