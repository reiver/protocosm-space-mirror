package librecords

import (
	"github.com/reiver/go-drisl"
)

// BlobCIDs returns the CIDs of the blobs a record refers to — at any depth — in order of first appearance, without duplicates.
//
// A blob reference is a map like:
//
//	{
//		"$type":    "blob",
//		"ref":      <CID>,
//		"mimeType": "image/png",
//		"size":     12345
//	}
//
// 'record' is a decoded record, as returned by drisl.Unmarshal.
//
// Example usage:
//
//	value, err := drisl.Unmarshal(drislBytes)
//	// ...
//	
//	cids := librecords.BlobCIDs(value)
func BlobCIDs(record any) []drisl.CID {
	var result []drisl.CID
	var seen map[string]struct{} = map[string]struct{}{}

	var walk func(any)
	walk = func(value any) {
		switch casted := value.(type) {
		case map[string]any:
			if "blob" == casted["$type"] {
				cid, found := casted["ref"].(drisl.CID)
				if found {
					var key string = string(cid)
					if _, found := seen[key]; !found {
						seen[key] = struct{}{}
						result = append(result, cid)
					}
				}
			}
			for _, item := range casted {
				walk(item)
			}
		case []any:
			for _, item := range casted {
				walk(item)
			}
		}
	}

	walk(record)
	return result
}
