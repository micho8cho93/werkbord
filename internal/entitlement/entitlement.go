// Package entitlement is what tells the Free plan from Premium.
//
// Werkbord is one product with one build. Free and Premium differ only by whether a person holds a valid unlock key (key.go),
// never by what was built or installed. A team of up to FreeTeamLimit people is free; a team of more than that needs every
// member to be Premium. This package is plumbing shared by both products: it holds no workspace, role, storage or process, and
// it names no service. The rules that apply it to a team are the Team service's (internal/team/service/plan.go).
package entitlement

// FreeTeamLimit is the most people a team may have without Premium: you and a partner.
const FreeTeamLimit = 2
