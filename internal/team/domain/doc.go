// Package domain is the Werkbord Team vocabulary: a workspace, its members, its
// projects, who belongs to which project, and what each role may do. It is pure
// rules, with no SQL, HTTP or I/O.
//
// A Team workspace is a COORDINATION layer. It records who is on the team and
// which projects exist, and it will carry the work they coordinate. It is not an
// execution environment: nothing in this package, or anywhere in Team, starts a
// process, holds a Git or GitHub credential, or reaches into another member's
// computer. Every member keeps running their own Werkbord runner with their own
// credentials (docs/PRODUCTS.md).
//
// A Team Project is not a Werkbord Project. The individual product's Project is a
// Git checkout on one computer; a Team Project is a name the team shares, with
// the repository's address if there is one. Each member's own Werkbord decides
// where, and whether, they have it checked out.
package domain
