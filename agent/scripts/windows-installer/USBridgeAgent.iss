; USBridge Agent setup for Windows (Inno Setup 6.3+).
;
; Built by scripts/build_windows_installer.sh from dist/windows (the same
; folder build_windows.sh zips), so the setup .exe and the portable .zip
; always carry identical files.
;
; What it does, in one elevated pass and without a reboot:
;   * copies the agent to Program Files;
;   * optionally installs the virtual display driver (MttVDD, via the same
;     install-mttvdd.ps1 the agent's own "Install" button runs) and the
;     usbip-win2 USB/IP driver (downloaded from its GitHub release and
;     installed silently with only the driver + CLI components -- no GUI,
;     SDK or debug symbols);
;   * pre-selects the USBridge streamer (RustShine) and USB passthrough in
;     the service's config, so the agent downloads both on its first start;
;   * opens the firewall for the agent and the streamers it downloads (a
;     LocalSystem service never gets the interactive "allow access?" prompt);
;   * registers and starts the USBridgeAgent service.

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\windows"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist"
#endif

#define AppName "USBridge Agent"
#define AppExeName "USBridgeAgent.exe"
#define ServiceName "USBridgeAgent"

; usbip-win2 release pinned by version and SHA-256 (attestation-signed by
; Microsoft, so no test-signing mode is needed). Bump both together.
#define UsbipVersion "0.9.8.1"
#define UsbipUrl "https://github.com/vadimgrn/usbip-win2/releases/download/v." + UsbipVersion + "/USBip-" + UsbipVersion + "-x64.exe"
#define UsbipSha256 "38cad6d4432b52d5bb9409d9ad03b72fdffc4ada4cd3a48fbeca1a2752a8518a"
; usbip-win2's own Inno Setup AppId -- used to detect an existing install.
#define UsbipAppId "{199505b0-b93d-4521-a8c7-897818e0205a}_is1"

[Setup]
AppId={{6B1F3C7E-2D84-4F0A-9E51-8C7A0D4B2E19}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher=USBridge Technologies
AppPublisherURL=https://www.usbridge.io
AppSupportURL=https://www.usbridge.io
DefaultDirName={autopf}\USBridge Agent
DefaultGroupName=USBridge
DisableProgramGroupPage=yes
DisableWelcomePage=yes
DisableDirPage=auto
DisableReadyPage=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
MinVersion=10.0.18362
WizardStyle=modern
SetupIconFile=..\..\cmd\usbridge_agent\appicon.ico
UninstallDisplayIcon={app}\{#AppExeName}
UninstallDisplayName={#AppName}
OutputDir={#OutputDir}
OutputBaseFilename=USBridgeAgent-Windows-x86_64-Setup-{#AppVersion}
Compression=lzma2/max
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
; Every driver here installs without one; never ask for a reboot.
RestartIfNeededByRun=no
VersionInfoVersion={#AppVersion}

[Tasks]
Name: "vdd"; Description: "Virtual display driver (stream without a monitor attached)"
Name: "usbip"; Description: "USB passthrough driver (usbip-win2)"
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; Flags: unchecked

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Excludes: "config.yaml,logs\*"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\..\internal\vdisplay\install-mttvdd.ps1"; DestDir: "{app}\drivers"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\{#AppName}"; Filename: "{app}\{#AppExeName}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
; The service already brings up a tray icon in the user's session; this only
; opens the main window once, as the user (not elevated).
Filename: "{app}\{#AppExeName}"; Description: "Open {#AppName}"; Flags: postinstall nowait skipifsilent runasoriginaluser

[UninstallRun]
Filename: "{app}\{#AppExeName}"; Parameters: "--uninstall-service"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveService"
Filename: "{sys}\taskkill.exe"; Parameters: "/F /T /IM usbridge-streamer.exe /IM usbridge-usb-broker.exe /IM sunshine.exe /IM punktfunk-host.exe /IM {#AppExeName}"; Flags: runhidden; RunOnceId: "KillAgent"
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""USBridge Agent"""; Flags: runhidden; RunOnceId: "RemoveFirewall"

[Code]
const
  // LocalSystem's roaming profile: where the service keeps config.yaml and
  // everything it downloads (os.UserConfigDir() under LocalSystem).
  ServiceStateDir = '{sys}\config\systemprofile\AppData\Roaming\usbridge-agent';

function Exec0(const Exe, Params: String): Integer;
var
  Code: Integer;
begin
  if not Exec(Exe, Params, '', SW_HIDE, ewWaitUntilTerminated, Code) then
    Code := -1;
  Log(Format('%s %s -> %d', [Exe, Params, Code]));
  Result := Code;
end;

procedure SetStatus(const Msg: String);
begin
  WizardForm.StatusLabel.Caption := Msg;
  WizardForm.FilenameLabel.Caption := '';
end;

// Stops the running service and every process it owns, so the files can be
// replaced on an upgrade.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Exec0(ExpandConstant('{sys}\sc.exe'), 'stop {#ServiceName}');
  Exec0(ExpandConstant('{sys}\taskkill.exe'), '/F /T /IM usbridge-streamer.exe /IM usbridge-usb-broker.exe /IM sunshine.exe /IM punktfunk-host.exe /IM {#AppExeName}');
  Result := '';
end;

function UsbipInstalled(): Boolean;
begin
  Result := RegKeyExists(HKLM64, 'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{#UsbipAppId}');
end;

procedure InstallUsbip();
var
  Setup: String;
begin
  if UsbipInstalled() then begin
    Log('usbip-win2 already installed, skipping');
    exit;
  end;
  SetStatus('Downloading the USB passthrough driver...');
  try
    DownloadTemporaryFile('{#UsbipUrl}', 'USBip-setup.exe', '{#UsbipSha256}', nil);
  except
    MsgBox('Could not download the USB passthrough driver:' + #13#10 + GetExceptionMessage + #13#10#13#10 +
           'USBridge will still work; install it later from the agent window.', mbError, MB_OK);
    exit;
  end;
  Setup := ExpandConstant('{tmp}\USBip-setup.exe');
  SetStatus('Installing the USB passthrough driver...');
  // Driver + usbip.exe only (both components are "fixed" in its setup);
  // vcredist is needed by its devnode.exe/usbip.exe.
  if Exec0(Setup, '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /SP- /NOICONS /COMPONENTS="main,client" /TASKS="vcredist" /LOG="' + ExpandConstant('{tmp}\usbip-setup.log') + '"') <> 0 then
    MsgBox('The USB passthrough driver setup failed. See ' + ExpandConstant('{tmp}\usbip-setup.log') + '.', mbError, MB_OK);
end;

procedure InstallVirtualDisplay();
var
  LogPath: String;
begin
  SetStatus('Installing the virtual display driver...');
  LogPath := ExpandConstant('{app}\logs\install-mttvdd.log');
  ForceDirectories(ExtractFileDir(LogPath));
  if Exec0(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
       '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "' + ExpandConstant('{app}\drivers\install-mttvdd.ps1') + '" -LogPath "' + LogPath + '"') <> 0 then
    MsgBox('The virtual display driver could not be installed. See ' + LogPath + '.', mbError, MB_OK);
end;

// Seeds the service's config.yaml with this setup's choices, without
// touching any value the user already set on a previous install.
procedure SeedServiceConfig();
var
  Dir, Path: String;
  Lines: TArrayOfString;
  Text: AnsiString;
begin
  Dir := ExpandConstant(ServiceStateDir);
  Path := Dir + '\config.yaml';
  ForceDirectories(Dir);
  Text := '';
  if FileExists(Path) then
    LoadStringFromFile(Path, Text);
  if Pos('preferred_backend:', Text) = 0 then
    SaveStringToFile(Path, #13#10 + 'preferred_backend: rustshine' + #13#10, True);
  if WizardIsTaskSelected('usbip') and (Pos('usb_broker_consent:', Text) = 0) then
    SaveStringToFile(Path, 'usb_broker_consent: true' + #13#10, True);
end;

procedure AllowFirewall(const Exe: String);
begin
  Exec0(ExpandConstant('{sys}\netsh.exe'),
    'advfirewall firewall add rule name="USBridge Agent" dir=in action=allow profile=any enable=yes program="' + Exe + '"');
end;

procedure OpenFirewall();
var
  State: String;
begin
  SetStatus('Configuring Windows Firewall...');
  Exec0(ExpandConstant('{sys}\netsh.exe'), 'advfirewall firewall delete rule name="USBridge Agent"');
  State := ExpandConstant(ServiceStateDir);
  AllowFirewall(ExpandConstant('{app}\{#AppExeName}'));
  AllowFirewall(State + '\usbridge-streamer\usbridge-streamer.exe');
  AllowFirewall(State + '\usb-broker\usbridge-usb-broker.exe');
  AllowFirewall(State + '\sunshine\sunshine.exe');
  AllowFirewall(State + '\punktfunk\punktfunk-host.exe');
end;

procedure StartService();
begin
  SetStatus('Starting the USBridge service...');
  if Exec0(ExpandConstant('{app}\{#AppExeName}'), '--install-service') <> 0 then begin
    MsgBox('Could not register the USBridge Agent service.', mbError, MB_OK);
    exit;
  end;
  // Come back by itself after a crash or a failed self-update.
  Exec0(ExpandConstant('{sys}\sc.exe'), 'failure {#ServiceName} reset= 86400 actions= restart/5000/restart/5000/restart/30000');
  Exec0(ExpandConstant('{sys}\sc.exe'), 'start {#ServiceName}');
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep <> ssPostInstall then
    exit;
  WizardForm.ProgressGauge.Style := npbstMarquee;
  if WizardIsTaskSelected('usbip') then
    InstallUsbip();
  if WizardIsTaskSelected('vdd') then
    InstallVirtualDisplay();
  SeedServiceConfig();
  OpenFirewall();
  StartService();
  WizardForm.ProgressGauge.Style := npbstNormal;
end;
