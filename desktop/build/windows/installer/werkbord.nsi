Unicode true
!include "MUI2.nsh"

!ifndef VERSION
  !error "VERSION is required"
!endif
!ifndef APP_SRC
  !error "APP_SRC is required"
!endif
!ifndef APP_ICON
  !error "APP_ICON is required"
!endif
!ifndef OUTPUT_FILE
  !error "OUTPUT_FILE is required"
!endif

!define PRODUCT "Werkbord"
!define UNINSTALL_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Werkbord"

Name "${PRODUCT}"
Caption "Werkbord ${VERSION} Setup"
OutFile "${OUTPUT_FILE}"
InstallDir "$PROGRAMFILES64\Werkbord"
RequestExecutionLevel admin
SetCompressor /SOLID lzma
SetRegView 64

VIProductVersion "${VERSION}.0"
VIFileVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "Werkbord"
VIAddVersionKey "FileDescription" "Werkbord Setup"
VIAddVersionKey "CompanyName" "Werkbord"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "Copyright © 2026 Werkbord"

!define MUI_ICON "${APP_ICON}"
!define MUI_UNICON "${APP_ICON}"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\bin\Werkbord.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Launch Werkbord"
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section "Install"
  SetShellVarContext all
  SetOutPath "$INSTDIR"
  File /r "${APP_SRC}\*"

  CreateDirectory "$SMPROGRAMS\Werkbord"
  CreateShortcut "$SMPROGRAMS\Werkbord\Werkbord.lnk" "$INSTDIR\bin\Werkbord.exe"
  CreateShortcut "$DESKTOP\Werkbord.lnk" "$INSTDIR\bin\Werkbord.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  WriteRegStr HKLM "${UNINSTALL_KEY}" "DisplayName" "Werkbord"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "Publisher" "Werkbord"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTALL_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegDWORD HKLM "${UNINSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTALL_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext all
  Delete "$DESKTOP\Werkbord.lnk"
  Delete "$SMPROGRAMS\Werkbord\Werkbord.lnk"
  RMDir "$SMPROGRAMS\Werkbord"
  DeleteRegKey HKLM "${UNINSTALL_KEY}"
  RMDir /r "$INSTDIR"
SectionEnd
