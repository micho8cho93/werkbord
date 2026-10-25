package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestProductionTeamAlwaysHoldsTheFreePlanSizeAndDeviceAuthentication(t *testing.T) {
	root := moduleRoot(t)
	for path, need := range map[string][]string{
		"internal/team/config/config.go":         {"c.Production = true", `c.KeyStorage = "os"`},
		"internal/team/server/server.go":         {"RequireDeviceProof: true", "configureTeamSize(svc, cfg)"},
		"internal/team/server/storage_create.go": {"configureTeamSize(w.Service, cfg)"},
		"internal/team/server/daemon.go":         {"o.Config.Production = true"},
		"internal/team/server/teamsize.go":       {"svc.EnforceTeamSize(entitlement.TeamLimit(cfg.Premium))"},
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

// Werkbord is one build. Free and Premium differ only by an unlock key, never by what was built, so nothing that ships may
// select an edition. The one seam is the test-only `-X main.testPremium=1` linker flag of the browser scripts that need a
// team larger than the Free plan's two people; no release, packaging or CI script may set it, and no source file may be
// built only for an edition.
func TestThereIsOneBuildAndNoEditionIsSelectedAtBuildTime(t *testing.T) {
	root := moduleRoot(t)
	var release []string
	for _, pattern := range []string{"scripts/build-*.sh", "scripts/build-*.ps1", "scripts/team-build-flags.sh", "scripts/release-*", "scripts/prepare-*", ".github/workflows/*.yml", "Makefile"} {
		m, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range m {
			if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
				release = append(release, f)
			}
		}
	}
	if len(release) < 4 {
		t.Fatalf("found only %d build and release files: the check is not looking in the right place", len(release))
	}
	edition := regexp.MustCompile(`(?i)testPremium|-tags[ =]+\S*(premium|edition|free)|main\.(edition|premium)\b|\bPREMIUM_BUILD\b`)
	for _, path := range release {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if m := edition.Find(b); m != nil {
			t.Errorf("%s selects an edition at build time (%q): there is one build, and an unlock key is the only difference between Free and Premium", path, m)
		}
	}
	buildTag := regexp.MustCompile(`(?m)^//go:build .*\b(premium|edition|free)\b`)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "third_party" || d.Name() == "dist") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if buildTag.Match(b) {
			t.Errorf("%s is built only for an edition: there is one build", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
