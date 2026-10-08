//go:build darwin && cgo

package pki

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#cgo CFLAGS: -Wno-deprecated-declarations
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
static void wb_zero(void *p, size_t n) { volatile unsigned char *q = p; while(n--) *q++ = 0; }

// All C allocations are freed here or through wb_free; no general native API is exposed.
static char *wb_secure_key(char *service, int *status) {
    SecKeychainRef kc = NULL;
    OSStatus err = noErr;
    if (geteuid() == 0) err = SecKeychainOpen("/Library/Keychains/System.keychain", &kc);
    void *data = NULL; UInt32 length = 0;
    if (err == noErr) err = SecKeychainFindGenericPassword(kc, (UInt32)strlen(service), service, 7, "sealing", &length, &data, NULL);
    unsigned char key[32];
    if (err == errSecItemNotFound) {
        err = SecRandomCopyBytes(kSecRandomDefault, sizeof(key), key);
        if (err == noErr) err = SecKeychainAddGenericPassword(kc, (UInt32)strlen(service), service, 7, "sealing", sizeof(key), key, NULL);
        if (err == errSecDuplicateItem) err = SecKeychainFindGenericPassword(kc, (UInt32)strlen(service), service, 7, "sealing", &length, &data, NULL);
    }
    if (data != NULL) {
        if (length != sizeof(key)) err = errSecDecode;
        else memcpy(key, data, sizeof(key));
        SecKeychainItemFreeContent(NULL, data);
    }
    char *out = NULL;
    if (err == noErr) {
        out = malloc(65);
        if (out == NULL) err = errSecAllocate;
        else { const char *hex = "0123456789abcdef"; for (int i=0;i<32;i++) { out[2*i]=hex[key[i]>>4]; out[2*i+1]=hex[key[i]&15]; } out[64]=0; }
    }
    wb_zero(key, sizeof(key));
    if (kc != NULL) CFRelease(kc);
    free(service); *status = (int)err; return out;
}
static void wb_free(char *p) { if (p != NULL) { wb_zero(p, strlen(p)); free(p); } }
*/
import "C"

import (
	"encoding/hex"
	"fmt"
)

func secureKey(service string) ([]byte, error) {
	var status C.int
	p := C.wb_secure_key(C.CString(service), &status)
	if status != 0 {
		return nil, fmt.Errorf("OS secure storage refused access (Keychain status %d); unlock the service's keychain or supply its external passphrase; secrets were not read from a fallback file", int(status))
	}
	defer C.wb_free(p)
	return hex.DecodeString(C.GoString(p))
}
