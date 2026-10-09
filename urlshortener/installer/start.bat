@echo off
set DATA_FILE=%LOCALAPPDATA%\URLShortener\links.json
set BASE_URL=http://localhost:8080
start "" cmd /c "timeout /t 2 >nul & start http://localhost:8080"
"%~dp0shortener.exe"