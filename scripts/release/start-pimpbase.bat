@echo off
rem Starts Pimpbase with its data in the "data" folder next to this file,
rem and opens it in the browser. Close this window (or press Ctrl+C) to stop.
cd /d "%~dp0"
start "" /b cmd /c "ping -n 4 127.0.0.1 >nul & start http://localhost:8080"
pimpbase.exe serve --data "%~dp0data" %*
if errorlevel 1 pause
