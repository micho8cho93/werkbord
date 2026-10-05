package domain

import "strings"

// RepositoryKey reduces a repository address to a form that is the same for every
// way of writing it, so a project's address and a local checkout's remote can be
// compared: https://github.com/Acme/Shop.git, git@github.com:acme/shop and
// ssh://git@github.com/acme/shop all become "github.com/acme/shop". The host is
// compared case-insensitively and the path as written; an address it cannot read
// gives "".
func RepositoryKey(addr string) string {
	s := strings.TrimSpace(addr)
	if s == "" {
		return ""
	}
	var host, path string
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if at := strings.LastIndexByte(rest, '@'); at >= 0 && at < strings.IndexByte(rest+"/", '/') {
			rest = rest[at+1:]
		}
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return ""
		}
		host, path = rest[:slash], rest[slash+1:]
	} else {
		at := strings.IndexByte(s, '@')
		colon := strings.IndexByte(s, ':')
		if colon < 0 || colon < at {
			return ""
		}
		host, path = s[at+1:colon], s[colon+1:]
	}
	if colon := strings.LastIndexByte(host, ':'); colon >= 0 { // drop a port
		host = host[:colon]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if host == "" || path == "" {
		return ""
	}
	return strings.ToLower(host) + "/" + path
}
