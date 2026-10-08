package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductionTeamAlwaysInstallsOfflineLicenseAndDeviceAuthentication(t *testing.T) {
	root := moduleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal/team")); os.IsNotExist(err) {
		t.Skip("Team is not in this tree (isolation check)")
	}
	for path, need := range map[string][]string{
		"internal/team/config/config.go":         {"c.LicenseRequired = true", `c.KeyStorage = "os"`},
		"internal/team/server/server.go":         {"RequireDeviceProof: true", "configureLicense(svc, cfg)"},
		"internal/team/server/storage_create.go": {"configureLicense(w.Service, cfg)"},
		"internal/team/server/daemon.go":         {"o.Config.LicenseRequired = true"},
		"internal/team/server/license.go":        {"svc.EnforceLicense(cfg.LicenseKey, raw)"},
		"internal/team/infra/overlay/overlay.go": {`root = root.add("sshd", tree{}.add("enabled", false))`},
	} {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range need {
			if !strings.Contains(string(b), text) {
				t.Errorf("%s lost its production guard %q", path, text)
			}
		}
	}
}

func TestNativeSecureStorageCannotGrowProcessOrNetworkCapabilities(t *testing.T) {
	if _, err := os.Stat(filepath.Join(moduleRoot(t), "internal/team")); os.IsNotExist(err) {
		t.Skip("Team is not in this tree (isolation check)")
	}
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), "internal/team/infra/pki/secure_darwin.go"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(b), "/*", 2)
	if len(parts) != 2 {
		t.Fatal("missing reviewed native secure storage")
	}
	code := strings.SplitN(parts[1], "*/", 2)[0]
	allowed := map[string]bool{"wb_zero": true, "wb_secure_key": true, "wb_free": true, "if": true, "for": true, "while": true, "sizeof": true, "geteuid": true, "SecKeychainOpen": true, "SecKeychainFindGenericPassword": true, "SecRandomCopyBytes": true, "SecKeychainAddGenericPassword": true, "SecKeychainItemFreeContent": true, "strlen": true, "memcpy": true, "malloc": true, "free": true, "CFRelease": true}
	code = regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(code, "")
	for _, m := range regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`).FindAllStringSubmatch(code, -1) {
		if !allowed[m[1]] {
			t.Errorf("native secure storage calls unreviewed %s; only Keychain, random bytes and memory operations belong here", m[1])
		}
	}
}
