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
