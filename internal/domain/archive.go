package domain

import (
	"fmt"
	"time"
)

// A task is Done, then archived, then deleted; a project is active, then archived, then deleted. Deleting is not instant: a deleted
// task or project waits in the trash for TrashRetention, where it can be restored whole, and is erased after that.

// TrashRetention is how long a deleted task or project can be restored before it is erased.
const TrashRetention = 30 * 24 * time.Hour

// SettingArchivePrefix is the settings key prefix for a project's ArchiveSettings: the project ID follows.
const SettingArchivePrefix = "archive:"

// ArchiveSettings are a project's choice about finished work. The zero value never archives by itself.
type ArchiveSettings struct {
	// AfterDays archives a Done task this many days after it was last changed. 0 means never.
	AfterDays int `json:"afterDays"`
}

// ArchiveAfterChoices are the periods a person can choose, in days; 0 is "never".
var ArchiveAfterChoices = []int{0, 7, 14, 30, 90}

func (a ArchiveSettings) Validate() error {
	for _, d := range ArchiveAfterChoices {
		if a.AfterDays == d {
			return nil
		}
	}
	return fmt.Errorf("%w: archive after 0, 7, 14, 30 or 90 days", ErrInvalid)
}

// ErasesAt is when something deleted at the given time is erased for good.
func ErasesAt(deleted time.Time) time.Time { return deleted.Add(TrashRetention) }
