// See updater_darwin.h and docs/DESKTOP.md ("Updates").
//
// Sparkle is not linked: it is loaded by name from Contents/Frameworks/Sparkle.framework, so that the app builds with no
// Sparkle on the machine (make desktop-check, development, CI), and so that if it is not there, or cannot load, the window
// opens and works exactly as it would without. Its Objective-C API is called by selector for the same reason.
//
// What it is set up to do and not do:
//   - It has no schedule. automaticallyChecksForUpdates is off in Info.plist and here, so nothing is ever asked of the
//     internet unless a person (or the web app's "Update now" for them) asks. The one automatic request Werkbord makes
//     is the controller's, which has its own switch (noUpdateCheck); and wbUpdaterAllowed is that same switch for this one.
//   - It installs only an update whose EdDSA signature verifies with the public key in Info.plist (SUPublicEDKey),
//     from a feed that is itself signed (SURequireSignedFeed), over https, and never a downgrade (Sparkle's own check).
//   - It does not replace the app while the program is being updated (shouldPostponeRelaunch).
#import <Cocoa/Cocoa.h>
#import <objc/message.h>

#include "updater_darwin.h"
#include "_cgo_export.h"

#pragma clang diagnostic ignored "-Wundeclared-selector"

static void wbEvent(int kind, int token, NSString *text) {
	char *c = (char *)[(text ?: @"") UTF8String];
	wbUpdaterEvent(kind, token, c);
}

// ---- the application menu ----

@interface WBMenuTarget : NSObject
- (void)checkForUpdates:(id)sender;
@end
@implementation WBMenuTarget
- (void)checkForUpdates:(id)sender {
	wbMenuCheckForUpdates();
}
@end
static WBMenuTarget *menuTarget;

static BOOL wbAddMenuItem(void) {
	NSMenu *main = NSApp.mainMenu;
	if (main == nil || main.numberOfItems == 0) return NO;
	NSMenu *app = [main itemAtIndex:0].submenu;
	if (app == nil) return NO;
	NSInteger about = -1;
	for (NSInteger i = 0; i < app.numberOfItems; i++) {
		NSMenuItem *item = [app itemAtIndex:i];
		if (item.target == menuTarget && item.action == @selector(checkForUpdates:)) return YES; // already there
		if (about < 0 && [item.title hasPrefix:@"About"]) about = i;
	}
	if (about < 0) return NO;
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:@"Check for Updates…" action:@selector(checkForUpdates:) keyEquivalent:@""];
	item.target = menuTarget;
	[app insertItem:item atIndex:about + 1];
	return YES;
}

#ifdef WB_UPDATER_TEST
// TEST BUILDS ONLY: what the menu bar holds, so that a test can see it without a screen (accessibility permission) to look at.
static void wbDumpMenus(void) {
	NSMutableString *s = [NSMutableString new];
	for (NSMenuItem *top in NSApp.mainMenu.itemArray) {
		[s appendFormat:@"[%@:", top.submenu.title ?: top.title ?: @""];
		for (NSMenuItem *it in top.submenu.itemArray) [s appendFormat:@" %@;", it.isSeparatorItem ? @"-" : it.title];
		[s appendString:@"]"];
	}
	wbEvent(EventTestMenu, 0, s);
}
#endif

static void wbTryAddMenuItem(int attempt) {
	if (wbAddMenuItem()) {
		wbEvent(EventMenuAdded, 0, @"");
#ifdef WB_UPDATER_TEST
		wbDumpMenus();
#endif
		return;
	}
	if (attempt >= 40) { // 8 seconds: the menu is built when the app has finished launching
		wbEvent(EventMenuFailed, 0, @"the application menu has no About item to put it after");
		return;
	}
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(200 * NSEC_PER_MSEC)), dispatch_get_main_queue(), ^{ wbTryAddMenuItem(attempt + 1); });
}

void wbInstallCheckForUpdatesMenuItem(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (menuTarget == nil) menuTarget = [WBMenuTarget new];
		wbTryAddMenuItem(0);
	});
}

// ---- Sparkle ----

@interface WBUpdater : NSObject
@property(strong) id controller; // SPUStandardUpdaterController
@property(strong) id updater;    // its SPUUpdater
@property int probeToken;
@property(copy) NSString *found;
@end

static WBUpdater *wb;

static NSString *describe(NSError *e) {
	if (e == nil) return @"";
	return [NSString stringWithFormat:@"%@ (%@ %ld)", e.localizedDescription ?: @"", e.domain, (long)e.code];
}

@implementation WBUpdater

// Is this person allowing looking for updates? The same switch the controller honours: noUpdateCheck / WERKBORD_NO_UPDATE_CHECK.
- (BOOL)updater:(id)updater mayPerformUpdateCheck:(NSInteger)check error:(NSError *__autoreleasing *)error {
	if (!wbUpdaterAllowed()) {
		if (error) *error = [NSError errorWithDomain:@"Werkbord" code:1 userInfo:@{NSLocalizedDescriptionKey : @"Looking for updates is turned off (noUpdateCheck in config.json)."}];
		return NO;
	}
	return YES;
}

- (void)updater:(id)updater didFindValidUpdate:(id)item {
	self.found = [item valueForKey:@"displayVersionString"];
	wbEvent(EventFound, 0, self.found);
}
- (void)updater:(id)updater didDownloadUpdate:(id)item { wbEvent(EventDownloaded, 0, [item valueForKey:@"displayVersionString"]); }
- (void)updater:(id)updater willExtractUpdate:(id)item { wbEvent(EventExtracting, 0, [item valueForKey:@"displayVersionString"]); }
- (void)updater:(id)updater willInstallUpdate:(id)item { wbEvent(EventInstalling, 0, [item valueForKey:@"displayVersionString"]); }
- (void)updaterWillRelaunchApplication:(id)updater { wbEvent(EventRelaunching, 0, @""); }
- (void)updater:(id)updater didAbortWithError:(NSError *)error {
	if (error.code == 1001) return; // SUNoUpdateError: there was nothing newer, which is not a failure
	wbEvent(EventAborted, 0, describe(error));
}

// The app is not swapped for its new version while the program is being updated: it waits, and goes on when that is over.
- (BOOL)updater:(id)updater shouldPostponeRelaunchForUpdate:(id)item untilInvokingBlock:(void (^)(void))installHandler {
	if (wbUpdaterMayRelaunch()) return NO;
	wbEvent(EventPostponed, 0, @"");
	[self waitThen:installHandler];
	return YES;
}
- (void)waitThen:(void (^)(void))handler {
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(2 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
		if (wbUpdaterMayRelaunch()) handler(); else [self waitThen:handler];
	});
}

// The answer to a probe: the end of a check that asked for information only.
- (void)updater:(id)updater didFinishUpdateCycleForUpdateCheck:(NSInteger)check error:(NSError *)error {
	if (check != 2 /* SPUUpdateCheckUpdateInformation */ || self.probeToken == 0) return;
	int token = self.probeToken;
	self.probeToken = 0;
	NSString *found = self.found;
	self.found = nil;
	if (found != nil) wbEvent(EventProbeFound, token, found);
	else if (error == nil || error.code == 1001) wbEvent(EventProbeNone, token, @"");
	else wbEvent(EventProbeError, token, describe(error));
}

#ifdef WB_UPDATER_TEST
// TEST BUILDS ONLY (go build -tags updatertest, which scripts/build-desktop.sh refuses to put in a release): with
// WERKBORD_UPDATER_TEST set, the app checks once at start and installs a valid update without asking, so that
// scripts/test-desktop-update.sh can run the real updater with nobody to press a button. Nothing else differs.
- (BOOL)updater:(id)updater willInstallUpdateOnQuit:(id)item immediateInstallationBlock:(void (^)(void))immediateInstallHandler {
	if (getenv("WERKBORD_UPDATER_TEST") == NULL) return NO;
	wbEvent(EventTestInstall, 0, [item valueForKey:@"displayVersionString"]);
	immediateInstallHandler();
	return YES;
}
#endif

@end

#ifdef WB_UPDATER_TEST
static void wbPollTrigger(NSString *trigger) {
	dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(500 * NSEC_PER_MSEC)), dispatch_get_main_queue(), ^{
		if ([NSFileManager.defaultManager fileExistsAtPath:trigger]) {
			[NSFileManager.defaultManager removeItemAtPath:trigger error:nil];
			((void (*)(id, SEL))objc_msgSend)(wb.updater, NSSelectorFromString(@"checkForUpdatesInBackground"));
		}
		wbPollTrigger(trigger);
	});
}
#endif

int wbUpdaterStart(char *why, int whyLen) {
	__block int rc = WBUpdaterNotInThisBuild;
	__block NSString *reason = @"";
	void (^start)(void) = ^{
		NSBundle *app = NSBundle.mainBundle;
		NSString *key = [app objectForInfoDictionaryKey:@"SUPublicEDKey"];
		NSString *feed = [app objectForInfoDictionaryKey:@"SUFeedURL"];
		NSString *path = [app.privateFrameworksPath stringByAppendingPathComponent:@"Sparkle.framework"];
		if (key.length == 0 || feed.length == 0) { reason = @"this build has no update feed and signing key"; return; }
		if (![NSFileManager.defaultManager fileExistsAtPath:path]) { reason = @"this build does not carry Sparkle"; return; }
		NSBundle *framework = [NSBundle bundleWithPath:path];
		NSError *err = nil;
		if (framework == nil || ![framework loadAndReturnError:&err]) {
			rc = WBUpdaterCouldNotLoad;
			reason = [NSString stringWithFormat:@"Sparkle could not be loaded: %@", describe(err)];
			return;
		}
		Class cls = NSClassFromString(@"SPUStandardUpdaterController");
		if (cls == Nil) { rc = WBUpdaterCouldNotLoad; reason = @"Sparkle has no SPUStandardUpdaterController"; return; }
		wb = [WBUpdater new];
		id (*initWith)(id, SEL, BOOL, id, id) = (void *)objc_msgSend;
		wb.controller = initWith([cls alloc], NSSelectorFromString(@"initWithStartingUpdater:updaterDelegate:userDriverDelegate:"), NO, wb, nil);
		if (wb.controller == nil) { rc = WBUpdaterCouldNotStart; reason = @"Sparkle did not make an updater"; return; }
		wb.updater = [wb.controller valueForKey:@"updater"];
		// No schedule, whatever Info.plist or an old preference says.
		[wb.updater setValue:@NO forKey:@"automaticallyChecksForUpdates"];
#ifdef WB_UPDATER_TEST
		if (getenv("WERKBORD_UPDATER_TEST") != NULL) [wb.updater setValue:@YES forKey:@"automaticallyDownloadsUpdates"];
#endif
		((void (*)(id, SEL))objc_msgSend)(wb.controller, NSSelectorFromString(@"startUpdater"));
		rc = WBUpdaterStarted;
#ifdef WB_UPDATER_TEST
		// The test asks for a check by creating the file WERKBORD_UPDATER_TEST_TRIGGER names (it must wait until first-run setup and
		// its own preparations are done). It is not gated on noUpdateCheck here on purpose: Sparkle's own delegate refuses, as it does for a person.
		if (getenv("WERKBORD_UPDATER_TEST") != NULL && getenv("WERKBORD_UPDATER_TEST_TRIGGER") != NULL) {
			NSString *trigger = [NSString stringWithUTF8String:getenv("WERKBORD_UPDATER_TEST_TRIGGER")];
			wbPollTrigger(trigger);
		}
#endif
	};
	if (NSThread.isMainThread) start(); else dispatch_sync(dispatch_get_main_queue(), start);
	if (why != NULL && whyLen > 0) snprintf(why, (size_t)whyLen, "%s", reason.UTF8String);
	return rc;
}

void wbUpdaterProbe(int token) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (wb == nil) { wbEvent(EventProbeError, token, @"the updater is not running"); return; }
		wb.probeToken = token;
		wb.found = nil;
		((void (*)(id, SEL))objc_msgSend)(wb.updater, NSSelectorFromString(@"checkForUpdateInformation"));
	});
}

void wbUpdaterShow(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (wb == nil) return;
		[NSApp activateIgnoringOtherApps:YES];
		((void (*)(id, SEL, id))objc_msgSend)(wb.controller, NSSelectorFromString(@"checkForUpdates:"), nil);
	});
}
