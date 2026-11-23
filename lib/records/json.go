package librecords

import (
	"codeberg.org/reiver/go-erorr"
	"github.com/reiver/go-drisl"
)

// ToJSON renders a record — stored as DRISL bytes — as AT-Protocol JSON.
//
// Example usage:
//
//	j, err := librecords.ToJSON(drislBytes)
func ToJSON(drislBytes []byte) ([]byte, error) {
	value, err := drisl.Unmarshal(drislBytes)
	if nil != err {
		return nil, erorr.Wrap(err, "failed to decode record DRISL")
	}

	j, err := drisl.ToJSON(value)
	if nil != err {
		return nil, erorr.Wrap(err, "failed to render record as JSON")
	}

	return j, nil
}
