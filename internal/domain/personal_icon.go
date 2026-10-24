package domain

import (
	_ "embed"
	"encoding/json"
	"math/bits"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// One approved catalogue drives server assignment and browser rendering. Icon
// names are data identifiers, never SVG, URLs or arbitrary component imports.
//
//go:embed personal_icon_catalog.json
var personalIconCatalogueJSON []byte

type personalLibraryIcon struct {
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
}

var personalIconCatalogue = func() []personalLibraryIcon {
	var icons []personalLibraryIcon
	if err := json.Unmarshal(personalIconCatalogueJSON, &icons); err != nil {
		panic(err)
	}
	return icons
}()

var personalLegacyIcons = []string{"overview", "board", "calendar", "timeline", "activity", "analytics", "projects", "folder", "check", "people"}
var personalIconPattern = regexp.MustCompile(`^workspace-[01][0-9a-f]{6}$`)

// LegacyPersonalIcon remains readable in saved history and older API requests.
func LegacyPersonalIcon(icon string) bool {
	if !personalIconPattern.MatchString(icon) {
		return false
	}
	mask, err := strconv.ParseUint(icon[len("workspace-"):], 16, 32)
	n := bits.OnesCount32(uint32(mask))
	return err == nil && n >= 8 && n <= 17
}

func LibraryPersonalIcon(icon string) bool { return inPersonalList(icon, PersonalIcons()) }
func ValidPersonalIcon(icon string) bool {
	return icon == "auto" || LibraryPersonalIcon(icon) || LegacyPersonalIcon(icon) || inPersonalList(icon, personalLegacyIcons)
}

func personalIconWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		words[strings.TrimSuffix(word, "s")] = true
	}
	return words
}

// PersonalIconCandidates ranks name matches first, then view names and kinds.
// Catalogue order breaks ties deterministically. No model or network is needed.
func PersonalIconCandidates(name, context string) []string {
	title, detail := personalIconWords(name), personalIconWords(context)
	type candidate struct {
		icon  string
		score int
	}
	choices := make([]candidate, 0, len(personalIconCatalogue))
	for _, icon := range personalIconCatalogue {
		score := 0
		keywords := personalIconWords(strings.Join(icon.Keywords, " ") + " " + strings.ReplaceAll(icon.Name, "-", " "))
		for word := range keywords {
			if title[word] {
				score += 10
			}
			if detail[word] {
				score++
			}
		}
		choices = append(choices, candidate{"lucide-" + icon.Name, score})
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].score > choices[j].score })
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = c.icon
	}
	return out
}

func PersonalIconContext(spec PersonalWorkspaceSpec) string {
	var hints []string
	for _, view := range spec.Definition.Views {
		hints = append(hints, view.Name, view.Kind)
	}
	return strings.Join(hints, " ")
}

// DisplayPersonalIcon is a compatibility fallback for historical definitions.
// Live legacy identities are upgraded together when the database opens.
func DisplayPersonalIcon(icon, name string) string {
	if LibraryPersonalIcon(icon) {
		return icon
	}
	return PersonalIconCandidates(name, "")[0]
}

type PersonalWorkspaceIcon struct{ ID, Name, Icon string }
