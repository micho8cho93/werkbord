package domain

import (
	"fmt"
	"hash/fnv"
	"math/bits"
	"regexp"
	"strconv"
)

const PersonalIconMask = uint32(1<<25 - 1)

var personalIconPattern = regexp.MustCompile(`^workspace-[01][0-9a-f]{6}$`)

// GeneratedPersonalIcon is a closed 5×5 glyph grammar, never SVG or an app icon.
func GeneratedPersonalIcon(icon string) bool {
	if !personalIconPattern.MatchString(icon) {
		return false
	}
	mask, err := strconv.ParseUint(icon[len("workspace-"):], 16, 32)
	n := bits.OnesCount32(uint32(mask))
	return err == nil && n >= 8 && n <= 17
}

func ValidPersonalIcon(icon string) bool {
	return icon == "auto" || GeneratedPersonalIcon(icon) || inPersonalList(icon, PersonalIcons())
}

// PersonalIconCandidate is shared with the browser's safe legacy fallback.
func PersonalIconCandidate(name, previous string, attempt uint32) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name + "\x00" + previous))
	return fmt.Sprintf("workspace-%07x", (h.Sum32()+attempt*0x9e3779b9)&PersonalIconMask)
}

// DisplayPersonalIcon replaces legacy navigation icons without changing saved definitions.
func DisplayPersonalIcon(icon, name string) string {
	if GeneratedPersonalIcon(icon) {
		return icon
	}
	for attempt := uint32(0); ; attempt++ {
		candidate := PersonalIconCandidate(name, icon, attempt)
		if GeneratedPersonalIcon(candidate) {
			return candidate
		}
	}
}

type PersonalWorkspaceIcon struct{ ID, Name, Icon string }
