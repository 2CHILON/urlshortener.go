[Setup]
AppName=URL Shortener
AppVersion=1.0.0
DefaultDirName={autopf}\URLShortener
DefaultGroupName=URL Shortener
OutputDir=..\dist
OutputBaseFilename=URLShortener-Setup
PrivilegesRequired=lowest
Compression=lzma
SolidCompression=yes

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"

[Files]
Source: "..\dist\shortener.exe"; DestDir: "{app}"
Source: "start.bat"; DestDir: "{app}"

[Icons]
Name: "{group}\URL Shortener"; Filename: "{app}\start.bat"; WorkingDir: "{app}"
Name: "{autodesktop}\URL Shortener"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\start.bat"; Description: "Launch URL Shortener"; Flags: postinstall nowait skipifsilent