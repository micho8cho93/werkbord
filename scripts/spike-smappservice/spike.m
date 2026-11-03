// THROWAWAY SPIKE (docs/SPIKE_SMAPPSERVICE.md): the app side. It only registers, unregisters and reports on the daemon in its bundle.
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>

static const char *name(SMAppServiceStatus s) {
    switch (s) {
    case SMAppServiceStatusNotRegistered: return "notRegistered";
    case SMAppServiceStatusEnabled: return "enabled";
    case SMAppServiceStatusRequiresApproval: return "requiresApproval";
    case SMAppServiceStatusNotFound: return "notFound";
    }
    return "unknown";
}

int main(int argc, char **argv) {
    @autoreleasepool {
        NSString *cmd = argc > 1 ? @(argv[1]) : @"status";
        SMAppService *s = [SMAppService daemonServiceWithPlistName:@"dev.werkbord.spike.plist"];
        NSError *e = nil;
        if ([cmd isEqualToString:@"register"]) {
            BOOL ok = [s registerAndReturnError:&e];
            printf("register ok=%d status=%s error=%s\n", ok, name(s.status), e ? e.localizedDescription.UTF8String : "-");
        } else if ([cmd isEqualToString:@"unregister"]) {
            BOOL ok = [s unregisterAndReturnError:&e];
            printf("unregister ok=%d status=%s error=%s\n", ok, name(s.status), e ? e.localizedDescription.UTF8String : "-");
        } else if ([cmd isEqualToString:@"open-settings"]) {
            [SMAppService openSystemSettingsLoginItems];
            printf("opened\n");
        } else {
            printf("status=%s\n", name(s.status));
        }
    }
    return 0;
}
