package librecords

import (
	"github.com/reiver/go-opt"
)

// LabelCollection is the collection (NSID) of label records.
const LabelCollection string = "com.atproto.label.record"

// Label is a decoded com.atproto.label.record record.
type Label struct {
	// URI is the subject of the label (required).
	URI string

	// CID is the version of the subject (optional). If it is nothing, the label applies to every version.
	CID opt.Optional[string]

	// Val is the label value — ex: "!takedown" (required).
	Val string

	// CTS is the "created at" timestamp (required).
	CTS string

	// Exp is the "expires at" timestamp (optional).
	Exp opt.Optional[string]
}

// ParseLabel reads a label from a decoded com.atproto.label.record record ('record' as returned by drisl.Unmarshal).
//
// It returns [ErrNotLabel] if a required field is missing or has the wrong type.
//
// Example usage:
//
//	label, err := librecords.ParseLabel(value)
func ParseLabel(record any) (Label, error) {
	var nada Label

	m, casted := record.(map[string]any)
	if !casted {
		return nada, ErrNotLabel
	}

	var label Label

	{
		uri, casted := m["uri"].(string)
		if !casted || "" == uri {
			return nada, ErrNotLabel
		}
		label.URI = uri
	}
	{
		val, casted := m["val"].(string)
		if !casted || "" == val {
			return nada, ErrNotLabel
		}
		label.Val = val
	}
	{
		cts, casted := m["cts"].(string)
		if !casted || "" == cts {
			return nada, ErrNotLabel
		}
		label.CTS = cts
	}

	if value, found := m["cid"]; found {
		cid, casted := value.(string)
		if !casted {
			return nada, ErrNotLabel
		}
		label.CID = opt.Something(cid)
	}
	if value, found := m["exp"]; found {
		exp, casted := value.(string)
		if !casted {
			return nada, ErrNotLabel
		}
		label.Exp = opt.Something(exp)
	}

	return label, nil
}
