// See alerts_darwin.h and docs/DESKTOP.md ("Alerts").
//
// UserNotifications only works for an app bundle with an identifier: from `go run`, or any binary outside a .app, asking for
// the notification centre raises an exception. That is checked first, and such a build simply has no alerts.
#import <Cocoa/Cocoa.h>
#import <UserNotifications/UserNotifications.h>

#include "alerts_darwin.h"

static BOOL wbInBundle(void) {
	NSBundle *b = NSBundle.mainBundle;
	return b.bundleIdentifier.length > 0 && [b.bundlePath.pathExtension isEqualToString:@"app"];
}

static void wbOnMain(void (^block)(void)) {
	if ([NSThread isMainThread]) block();
	else dispatch_sync(dispatch_get_main_queue(), block);
}

// Clicking a notification brings the window back, which may be hidden (closing the window hides the app).
@interface WBAlertDelegate : NSObject <UNUserNotificationCenterDelegate>
@end
@implementation WBAlertDelegate
- (void)userNotificationCenter:(UNUserNotificationCenter *)center willPresentNotification:(UNNotification *)notification withCompletionHandler:(void (^)(UNNotificationPresentationOptions))handler {
	handler(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList | UNNotificationPresentationOptionSound);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center didReceiveNotificationResponse:(UNNotificationResponse *)response withCompletionHandler:(void (^)(void))handler {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp unhide:nil];
		[NSApp activateIgnoringOtherApps:YES];
		for (NSWindow *w in NSApp.windows) [w makeKeyAndOrderFront:nil];
	});
	handler();
}
@end

static WBAlertDelegate *alertDelegate;

static UNUserNotificationCenter *wbCenter(void) {
	static dispatch_once_t once;
	dispatch_once(&once, ^{
		alertDelegate = [WBAlertDelegate new];
		UNUserNotificationCenter.currentNotificationCenter.delegate = alertDelegate;
	});
	return UNUserNotificationCenter.currentNotificationCenter;
}

int wbAlertStatus(void) {
	if (!wbInBundle()) return WBAlertUnsupported;
	__block int out = WBAlertUnknown;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	[wbCenter() getNotificationSettingsWithCompletionHandler:^(UNNotificationSettings *s) {
		switch (s.authorizationStatus) {
		case UNAuthorizationStatusNotDetermined: out = WBAlertUnknown; break;
		case UNAuthorizationStatusDenied: out = WBAlertDenied; break;
		default: out = WBAlertGranted;
		}
		dispatch_semaphore_signal(done);
	}];
	dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 2 * NSEC_PER_SEC));
	return out;
}

int wbAlertShow(const char *title, const char *body, int urgent, int force) {
	if (!wbInBundle()) return 0;
	NSString *t = [NSString stringWithUTF8String:title ?: ""], *b = [NSString stringWithUTF8String:body ?: ""];
	__block BOOL active = NO;
	wbOnMain(^{ active = NSApp.isActive; });
	if (active && !force) return 1; // the person is looking at Werkbord: it already says so
	if (!active) {
		dispatch_async(dispatch_get_main_queue(), ^{ [NSApp requestUserAttention:urgent ? NSCriticalRequest : NSInformationalRequest]; });
	}
	__block int shown = 1;
	dispatch_semaphore_t done = dispatch_semaphore_create(0);
	UNUserNotificationCenter *center = wbCenter();
	[center requestAuthorizationWithOptions:(UNAuthorizationOptionAlert | UNAuthorizationOptionSound) completionHandler:^(BOOL granted, NSError *error) {
		if (!granted) {
			shown = 0;
		} else {
			UNMutableNotificationContent *content = [UNMutableNotificationContent new];
			content.title = t;
			content.body = b;
			content.sound = urgent ? UNNotificationSound.defaultSound : nil;
			UNNotificationRequest *req = [UNNotificationRequest requestWithIdentifier:NSUUID.UUID.UUIDString content:content trigger:nil];
			[center addNotificationRequest:req withCompletionHandler:^(NSError *e) { if (e) shown = 0; dispatch_semaphore_signal(done); }];
			return;
		}
		dispatch_semaphore_signal(done);
	}];
	// The first alert asks the person for permission, and they may take a while: that is not a failure to show it.
	dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC));
	return shown;
}

void wbAlertOpenSettings(void) {
	NSURL *url = [NSURL URLWithString:@"x-apple.systempreferences:com.apple.Notifications-Settings.extension"];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (![NSWorkspace.sharedWorkspace openURL:url]) {
			[NSWorkspace.sharedWorkspace openURL:[NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.notifications"]];
		}
	});
}
