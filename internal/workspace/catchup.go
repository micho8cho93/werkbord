package workspace

import "time"

// Change is a measured completion or merge. Key is stable across duplicate
// reports; Origin optionally links a local handoff to its coordination record.
type Change struct {
	ID     string    `json:"id"`
	Key    string    `json:"key"`
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Href   string    `json:"href"`
	Origin string    `json:"origin,omitempty"`
	At     time.Time `json:"at"`
}

// RecentChanges is a bounded historical projection. Since is the earliest
// covered instant; callers must not claim complete history before it.
type RecentChanges struct {
	Since      time.Time `json:"since"`
	Through    time.Time `json:"through"`
	Cursor     int64     `json:"cursor"`
	Items      []Change  `json:"items"`
	Incomplete bool      `json:"incomplete"`
}

func (c Change) valid() bool {
	return ValidID(c.ID) && len(c.Key) > 0 && plain(c.Key, 600) && plain(c.Origin, 600) &&
		(c.Kind == "completed" || c.Kind == "pr_merged") && c.Title != "" && plain(c.Title, MaxText) && ValidHref(c.Href) && !c.At.IsZero()
}

func (c *RecentChanges) valid() bool {
	if c == nil {
		return true
	}
	if c.Since.IsZero() || c.Through.Before(c.Since) || c.Cursor < 0 || len(c.Items) > MaxItems {
		return false
	}
	for _, it := range c.Items {
		if !it.valid() || it.At.Before(c.Since) || it.At.After(c.Through) {
			return false
		}
	}
	return true
}

// Push is a branch of a team's repository that moved, as a member's Werkbord saw it on GitHub. Key is stable, so a push told twice is one.
type Push struct {
	ID      string    `json:"id"`
	Key     string    `json:"key"`
	Project string    `json:"project"`
	Branch  string    `json:"branch"`
	SHA     string    `json:"sha"`
	Href    string    `json:"href"`
	At      time.Time `json:"at"`
	// Mine: the person it is told to is the one whose Werkbord reported it, who knows already.
	Mine bool `json:"mine,omitempty"`
}

// MaxPushes bounds how many recent pushes a summary carries.
const MaxPushes = 20

func (p Push) valid() bool {
	return ValidID(p.ID) && len(p.Key) > 0 && plain(p.Key, 600) && p.Project != "" && plain(p.Project, MaxName) && p.Branch != "" && plain(p.Branch, 200) &&
		len(p.SHA) >= 7 && len(p.SHA) <= 64 && ValidHref(p.Href) && !p.At.IsZero()
}

func validPushes(ps []Push) bool {
	if len(ps) > MaxPushes {
		return false
	}
	for _, p := range ps {
		if !p.valid() {
			return false
		}
	}
	return true
}
