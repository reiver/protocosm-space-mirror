package libchangelog

import (
	"time"

	"protocosm-space-mirror/lib/db"
	"protocosm-space-mirror/lib/xrpc"
)

// ChangeJSON is a change-log entry, as rendered for consumers.
type ChangeJSON struct {
	Seq        int64  `json:"seq"`
	At         string `json:"at"`
	Kind       string `json:"kind"`
	Scope      string `json:"scope,omitempty"`
	Space      string `json:"space,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Collection string `json:"collection,omitempty"`
	RKey       string `json:"rkey,omitempty"`
	CID        string `json:"cid,omitempty"`
	Rev        string `json:"rev,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ToJSON converts a change-log entry into the form consumers see.
func ToJSON(change libdb.Change) ChangeJSON {
	return ChangeJSON{
		Seq:        change.Seq,
		At:         time.Unix(change.At, 0).UTC().Format(time.RFC3339),
		Kind:       change.Kind,
		Scope:      change.Scope,
		Space:      change.SpaceURI,
		Repo:       change.DID,
		Collection: change.Collection,
		RKey:       change.RKey,
		CID:        change.CID,
		Rev:        change.Rev,
		Reason:     change.Reason,
	}
}

// Marshal renders a change-log entry as (compact, single-line) JSON.
func Marshal(change libdb.Change) ([]byte, error) {
	return libxrpc.MarshalJSON(ToJSON(change))
}
