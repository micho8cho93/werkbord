// Package entitlement holds the one rule that tells the Free plan from Premium: how many people a team may have.
//
// Werkbord is one product with one build. Free and Premium differ only by whether the person (and, for a team, each of its
// members) holds a valid unlock key, never by what was built or installed. This package is plumbing shared by both
// products: it holds no workspace, role, storage or process, and it names no service. Today nobody can unlock Premium (the
// unlock key arrives in a later stage), so production wiring always applies the Free limit.
package entitlement

import "math"

// FreeTeamLimit is the most people a team may have on the Free plan: you and a partner.
const FreeTeamLimit = 2

// TeamLimit is how many people a team may have. Premium has no limit.
func TeamLimit(premium bool) int {
	if premium {
		return math.MaxInt
	}
	return FreeTeamLimit
}

// AllowsTeamSize reports whether a team of size people is allowed.
func AllowsTeamSize(premium bool, size int) bool {
	return size <= TeamLimit(premium)
}
