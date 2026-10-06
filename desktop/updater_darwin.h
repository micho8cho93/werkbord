// The native half of the app's own updating (docs/DESKTOP.md, "Updates"): Sparkle, loaded at run time from
// Contents/Frameworks, and the "Check for Updates…" item of the application menu. All of it is reached from Go
// (updater_darwin.go) and may be called from any thread: it moves to the main thread itself.
#ifndef WERKBORD_UPDATER_DARWIN_H
#define WERKBORD_UPDATER_DARWIN_H

// What wbUpdaterStart can say. Anything but 0 leaves the app without Sparkle: it works the same, and updates the program as before.
enum {
	WBUpdaterStarted = 0,
	WBUpdaterNotInThisBuild = 1, // no Sparkle.framework in the bundle, or no feed address and signing key in its Info.plist
	WBUpdaterCouldNotLoad = 2,   // the framework is there and could not be loaded
	WBUpdaterCouldNotStart = 3,  // it loaded and Sparkle refused to start
};

// wbUpdaterStart loads Sparkle and starts its updater, which has NO schedule: it checks only when asked. why holds a
// sentence for the log when the result is not WBUpdaterStarted.
int wbUpdaterStart(char *why, int whyLen);

// wbUpdaterProbe asks the feed whether an update exists, with no window. The answer comes back as a wbUpdaterEvent
// (kind EventProbe*) carrying token. wbUpdaterShow opens Sparkle's own window (release notes, Install, Later).
void wbUpdaterProbe(int token);
void wbUpdaterShow(void);

// wbInstallCheckForUpdatesMenuItem adds "Check for Updates…" under "About Werkbord" in the application menu, the
// conventional place, and has it call wbMenuCheckForUpdates. It waits for the menu to exist.
void wbInstallCheckForUpdatesMenuItem(void);

// Event kinds, for wbUpdaterEvent(kind, token, text).
enum {
	EventProbeFound = 1,  // text: the version offered
	EventProbeNone = 2,   // the feed has nothing newer
	EventProbeError = 3,  // text: why
	EventFound = 10,      // an update was found (any kind of check); text: version
	EventDownloaded = 11, // the archive arrived
	EventExtracting = 12, // it is about to be unpacked (Sparkle checks its EdDSA signature first: SUVerifyUpdateBeforeExtraction)
	EventInstalling = 13, // text: version; the app is about to quit and be replaced
	EventRelaunching = 14,
	EventPostponed = 15,  // the relaunch waits for the program's own update to finish
	EventAborted = 16,    // text: the error (an update that is not valid is refused here, and nothing is installed)
	EventMenuAdded = 30,
	EventMenuFailed = 31,
	EventTestInstall = 40, // test builds only
	EventTestMenu = 41,    // test builds only: the menu bar's titles, to show there is one "Check for Updates…" and where
};

#endif
