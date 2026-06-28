@echo off
REM Install client dependencies reproducibly from package-lock.json.
REM All runtime/build dependencies are declared in package.json, so a single
REM `npm ci` installs everything (no global tools or ad-hoc packages needed).
call npm ci
if errorlevel 1 (
    echo npm ci failed.
    exit /b 1
)
pause

