; USBridge setup for Windows (Inno Setup 6.3+): the client, and optionally
; the agent, in one download. Both are ticked by default; the agent-only
; setup (agent/scripts/windows-installer/USBridgeAgent.iss) stays the
; lightweight download for machines that are only ever reached remotely.
;
; Built by release-all.yml's bundle-windows job:
;   ISCC /DAppVersion=<v> /DClientDir=<unzipped client portable folder>
;        /DAgentSetup=<USBridgeAgent-Windows-x86_64-Setup.exe> USBridge.iss
;
; The client installs per user into %LOCALAPPDATA%\Programs\USBridge Client:
; no UAC prompt for it, and its own self-update
; (client/internal/update/apply_windows.go) replaces its files without
; elevating. The client needs no drivers -- a USB device that needs a WinUSB
; rebind is elevated for in the app itself.
;
; The agent is its own setup, run silently with its defaults (virtual
; display + USB drivers, the service, USBridge Streamer selected); it asks
; for administrator rights itself, so that is the only UAC prompt, and only
; when the agent is ticked. It keeps its own uninstaller and self-update.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef ClientDir
  #define ClientDir "..\..\client\dist\windows"
#endif
#ifndef AgentSetup
  #define AgentSetup "..\..\agent\dist\USBridgeAgent-Windows-x86_64-Setup.exe"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

#define AppName "USBridge Client"
#define AppExe "bin\USBridge_Client.exe"

[Setup]
AppId={{3E8A51C2-7B4D-4C19-A6F0-5D92E1B7C843}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=USBridge Technologies
AppPublisherURL=https://www.usbridge.io
AppSupportURL=https://www.usbridge.io
DefaultDirName={autopf}\USBridge Client
DefaultGroupName=USBridge
DisableProgramGroupPage=yes
DisableWelcomePage=yes
DisableDirPage=yes
DisableReadyPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; Per user: {autopf} is %LOCALAPPDATA%\Programs.
PrivilegesRequired=lowest
MinVersion=10.0.18362
WizardStyle=modern
SetupIconFile=USBridge.ico
UninstallDisplayIcon={app}\{#AppExe}
UninstallDisplayName={#AppName}
OutputDir={#OutputDir}
OutputBaseFilename=USBridge-Windows-x86_64-Setup-{#AppVersion}
Compression=lzma2/max
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
VersionInfoVersion={#AppVersion}

[Types]
Name: "full"; Description: "Client and Agent"
Name: "custom"; Description: "Custom"; Flags: iscustom

[Components]
Name: "client"; Description: "USBridge Client - connect to your computers"; Types: full custom
Name: "agent"; Description: "USBridge Agent - let this computer be reached remotely (drivers + background service, asks for administrator rights)"; Types: full custom

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; Components: client

[Files]
; config.yaml is the user's own settings on an upgrade; the portable
; shortcut points at its own folder and is replaced by the ones below.
Source: "{#ClientDir}\*"; DestDir: "{app}"; Excludes: "config.yaml,logs\*,*.lnk"; Components: client; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#AgentSetup}"; DestDir: "{tmp}"; DestName: "USBridgeAgent-Setup.exe"; Components: agent; Flags: deleteafterinstall

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExe}"; WorkingDir: "{app}\bin"; Components: client
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExe}"; WorkingDir: "{app}\bin"; Tasks: desktopicon

[Run]
; shellexec: the agent setup needs administrator rights, and ShellExecute is
; what lets Windows show the UAC prompt for it from this unelevated setup.
Filename: "{tmp}\USBridgeAgent-Setup.exe"; Parameters: "/SILENT /SUPPRESSMSGBOXES /NORESTART"; StatusMsg: "Installing USBridge Agent..."; Components: agent; Flags: shellexec waituntilterminated
Filename: "{app}\{#AppExe}"; WorkingDir: "{app}\bin"; Description: "Open {#AppName}"; Components: client; Flags: postinstall nowait skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/F /T /IM USBridge_Client.exe"; Flags: runhidden; RunOnceId: "KillClient"

[Code]
// Closes a running client so its files can be replaced on an upgrade.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  if WizardIsComponentSelected('client') then
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM USBridge_Client.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Result := '';
end;
