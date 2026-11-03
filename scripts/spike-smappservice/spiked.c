// THROWAWAY SPIKE (docs/SPIKE_SMAPPSERVICE.md). A stand-in for Team's service: runs as root from inside the app bundle, mirrors how the
// real service keeps its sealing key in the System keychain (internal/team/infra/pki/secure_darwin.go), and answers two questions on
// loopback: what am I (version, user, path, what the keychain said) and "exit" (so launchd restarts whatever binary is on disk now).
// It is deliberately tiny and holds nothing that matters.
#include <Security/Security.h>
#include <arpa/inet.h>
#include <mach-o/dyld.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#ifndef SPIKE_VERSION
#define SPIKE_VERSION "unset"
#endif
#define SERVICE "werkbord-spike/sealing-item"
#define PORT 47633

static const unsigned char SECRET[32] = "spike-sealing-key-0123456789ab";
static char state[1024];
static int idle = 0; // after /cleanup: do not touch the keychain again

static void keychain_probe(int *find, int *add, int *matches) {
    *find = *add = 0; *matches = 0;
    SecKeychainRef kc = NULL;
    OSStatus err = noErr;
    if (geteuid() == 0) err = SecKeychainOpen("/Library/Keychains/System.keychain", &kc);
    void *data = NULL; UInt32 length = 0;
    if (err == noErr) err = SecKeychainFindGenericPassword(kc, (UInt32)strlen(SERVICE), SERVICE, 7, "sealing", &length, &data, NULL);
    *find = (int)err;
    if (err == errSecItemNotFound) {
        OSStatus a = SecKeychainAddGenericPassword(kc, (UInt32)strlen(SERVICE), SERVICE, 7, "sealing", sizeof(SECRET), SECRET, NULL);
        *add = (int)a;
        if (a == errSecDuplicateItem) { a = SecKeychainFindGenericPassword(kc, (UInt32)strlen(SERVICE), SERVICE, 7, "sealing", &length, &data, NULL); *find = (int)a; err = a; }
        else if (a == noErr) *matches = 1;
    }
    if (data != NULL) { *matches = length == sizeof(SECRET) && memcmp(data, SECRET, sizeof(SECRET)) == 0; SecKeychainItemFreeContent(NULL, data); }
    if (kc) CFRelease(kc);
}

static int keychain_delete(void) {
    SecKeychainRef kc = NULL; SecKeychainItemRef item = NULL;
    OSStatus err = geteuid() == 0 ? SecKeychainOpen("/Library/Keychains/System.keychain", &kc) : errSecInvalidKeychain;
    if (err == noErr) err = SecKeychainFindGenericPassword(kc, (UInt32)strlen(SERVICE), SERVICE, 7, "sealing", NULL, NULL, &item);
    if (err == noErr && item) err = SecKeychainItemDelete(item);
    if (item) CFRelease(item);
    if (kc) CFRelease(kc);
    return (int)err;
}

static void describe(void) {
    char exe[2048]; uint32_t n = sizeof(exe);
    if (_NSGetExecutablePath(exe, &n) != 0) strcpy(exe, "?");
    int find = 0, add = 0, matches = 0;
    if (!idle) keychain_probe(&find, &add, &matches);
    snprintf(state, sizeof(state), "{\"version\":\"%s\",\"euid\":%d,\"pid\":%d,\"exe\":\"%s\",\"at\":%ld,\"keychainFind\":%d,\"keychainAdd\":%d,\"keychainReadable\":%s}\n",
             SPIKE_VERSION, (int)geteuid(), (int)getpid(), exe, (long)time(NULL), find, add, matches ? "true" : "false");
    FILE *f = fopen("/var/tmp/werkbord-spike.log", "a");
    if (f) { fputs(state, f); fclose(f); chmod("/var/tmp/werkbord-spike.log", 0644); }
}

int main(void) {
    describe();
    int s = socket(AF_INET, SOCK_STREAM, 0), on = 1;
    setsockopt(s, SOL_SOCKET, SO_REUSEADDR, &on, sizeof(on));
    struct sockaddr_in a = { .sin_family = AF_INET, .sin_port = htons(PORT), .sin_addr.s_addr = htonl(INADDR_LOOPBACK) };
    if (bind(s, (struct sockaddr *)&a, sizeof(a)) != 0 || listen(s, 4) != 0) { perror("bind"); return 1; }
    for (;;) {
        int c = accept(s, NULL, NULL);
        if (c < 0) continue;
        char req[512] = {0};
        ssize_t got = read(c, req, sizeof(req) - 1);
        (void)got;
        char body[1100];
        int quit = 0;
        if (strncmp(req, "POST /exit", 10) == 0) { snprintf(body, sizeof(body), "{\"exiting\":true}\n"); quit = 1; }
        else if (strncmp(req, "POST /cleanup", 13) == 0) { int r = keychain_delete(); idle = 1; snprintf(body, sizeof(body), "{\"deleted\":%d}\n", r); }
        else snprintf(body, sizeof(body), "%s", state);
        char head[200];
        int hl = snprintf(head, sizeof(head), "HTTP/1.0 200 OK\r\nContent-Type: application/json\r\nContent-Length: %zu\r\nConnection: close\r\n\r\n", strlen(body));
        (void)!write(c, head, hl); (void)!write(c, body, strlen(body));
        close(c);
        if (quit) return 0;
    }
}
