@echo off
rem ---------------------------------------------------------------------------
rem Player side: joins a room and listens on localhost, so Minecraft only ever
rem sees "localhost" no matter where the host is or whether the relay is used.
rem
rem 1. put this file next to NoneFrp-windows-amd64-console.exe
rem 2. the club sets RELAY below before handing the zip out
rem 3. double click, paste the address the host sent, keep the window open
rem ---------------------------------------------------------------------------

rem The relay hostname, without https:// and without a port.
rem Whole line, example:  set RELAY=relay.example.com
set RELAY=relay.example.com

if "%RELAY%"=="relay.example.com" (
  echo.
  echo RELAY has not been set yet.
  echo Open this file in Notepad and replace relay.example.com with the club
  echo relay hostname, then save and run it again.
  echo.
  pause
  exit /b 1
)

set /p ROOM=Paste the room address the host sent (e.g. 7fcrfu.mc.example.com:25565): 
if "%ROOM%"=="" goto :eof

"%~dp0NoneFrp-windows-amd64-console.exe" join -relay "%RELAY%" -room "%ROOM%" -listen 127.0.0.1:25565
echo.
echo The room closed. Press any key to exit.
pause >nul
