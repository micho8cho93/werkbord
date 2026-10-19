// The native half of the window's alerts (docs/DESKTOP.md, "Alerts"): a notification from the Mac and a bounce of the Dock
// icon, when something happened while the person is in another app. Reached from Go (alerts_darwin.go) from any thread.
#ifndef WERKBORD_ALERTS_DARWIN_H
#define WERKBORD_ALERTS_DARWIN_H

// What the system allows this app to do. A build that is not an app bundle (go run, a development build) cannot post
// notifications at all: WBAlertUnsupported.
enum {
	WBAlertUnsupported = 0,
	WBAlertUnknown = 1, // the person has not been asked yet: the first alert asks
	WBAlertDenied = 2,  // refused in System Settings
	WBAlertGranted = 3,
};

int wbAlertStatus(void);

// wbAlertShow posts one notification and, when the app is behind another, asks for attention in the Dock (critical: until the
// person comes back; otherwise one bounce). With force it is posted even when the app is in front. It returns 1 when the system took
// it, or when asking the person is still pending, and 0 when it could not be shown.
int wbAlertShow(const char *title, const char *body, int urgent, int force);

// wbAlertOpenSettings opens the system's notification settings.
void wbAlertOpenSettings(void);

#endif
