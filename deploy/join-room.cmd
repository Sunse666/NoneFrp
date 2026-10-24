@echo off
rem Player side: joins a room and listens on localhost, so Minecraft only ever
rem sees "localhost" no matter where the host is or whether the relay is used.
rem
rem 1. put this file next to NoneFrp-windows-amd64-console.exe
rem 2. set RELAY once (the club edits this line before handing the zip out)
rem 3. double click, type the room code, leave the window open while playing

set RELAY=relay.example.com

set /p ROOM=Room code or address (e.g. 7fcrfu.mc.example.com:25565): 
if "%ROOM%"=="" goto :eof

"%~dp0NoneFrp-windows-amd64-console.exe" join -relay "%RELAY%" -room "%ROOM%" -listen 127.0.0.1:25565
echo.
echo The room closed. Press any key to exit.
pause >nul
