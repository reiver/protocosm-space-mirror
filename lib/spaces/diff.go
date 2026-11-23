package libspaces

import (
	"sort"

	"codeberg.org/reiver/go-erorr"
	"codeberg.org/reiver/go-field"
)

// Diff is the change from the spaces currently listed to the spaces in a spaces file.
type Diff struct {
	// Added are spaces not known before.
	Added []string

	// Removed are listed spaces that are no longer in the file (to be unlisted).
	Removed []string

	// Relisted are unlisted spaces that are in the file again.
	Relisted []string
}

// IsEmpty returns whether the diff changes nothing.
func (receiver Diff) IsEmpty() bool {
	return 0 == len(receiver.Added) && 0 == len(receiver.Removed) && 0 == len(receiver.Relisted)
}

// Compare compares the spaces in a spaces file with the known spaces ('known' maps each known space to whether it is listed).
// The lists in the result are sorted.
func Compare(fileSpaces []string, known map[string]bool) Diff {
	var diff Diff

	var inFile = map[string]struct{}{}
	for _, spaceURI := range fileSpaces {
		inFile[spaceURI] = struct{}{}

		listed, found := known[spaceURI]
		switch {
		case !found:
			diff.Added = append(diff.Added, spaceURI)
		case !listed:
			diff.Relisted = append(diff.Relisted, spaceURI)
		}
	}

	for spaceURI, listed := range known {
		if !listed {
			continue
		}
		if _, found := inFile[spaceURI]; !found {
			diff.Removed = append(diff.Removed, spaceURI)
		}
	}

	sort.Strings(diff.Added)
	sort.Strings(diff.Removed)
	sort.Strings(diff.Relisted)

	return diff
}

// CheckUnlist returns [ErrTooManyUnlisted] if the diff would unlist more than 'unlistMax' spaces,
// or more than half of the 'listedCount' spaces currently listed — unless 'force' is true.
//
// This guards against a wrong, empty, or truncated spaces file hiding (and later purging) many spaces at once.
func CheckUnlist(diff Diff, listedCount int, unlistMax int, force bool) error {
	if force {
		return nil
	}

	var removed int = len(diff.Removed)

	if unlistMax < removed || listedCount < removed*2 {
		return erorr.Wrap(ErrTooManyUnlisted, "refusing to unlist this many spaces without force",
			field.Int("unlisting", removed),
			field.Int("listed", listedCount),
			field.Int("unlist-max", unlistMax),
		)
	}

	return nil
}
