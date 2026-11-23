package libdb

// The visibility rules, as SQL.
//
// A space is visible when its lifecycle is 'active' and it has no 'space' hide.
// A repo is visible when its space is visible, and there is no 'account' hide on its DID, and no 'repo' hide on it.
// A record is visible when its repo is visible, it is in the repo's current generation, and there is no 'record' hide on it
// (a 'record' hide with a CID only hides that version of the record).
//
// Any hide reason counts — the reasons only matter for un-hiding, and for the operator.

// sqlSpaceVisible is true when the space whose URI is in column 's.space_uri' is visible.
// ('s' must be the spaces table.)
const sqlSpaceVisible string =
	`s.lifecycle = 'active'` +" "+
	`AND NOT EXISTS (`+
		`SELECT 1 FROM hides AS h WHERE h.scope_kind = 'space' AND h.space_uri = s.space_uri AND h.did = '' AND h.collection = '' AND h.rkey = '' AND h.cid = ''`+
	`)`

// sqlRepoVisible is true when the repo in row 'rp' (of the repos table) is visible — not counting its space.
const sqlRepoVisible string =
	`NOT EXISTS (`+
		`SELECT 1 FROM hides AS h WHERE h.scope_kind = 'account' AND h.space_uri = '' AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = ''`+
	`)` +" "+
	`AND NOT EXISTS (`+
		`SELECT 1 FROM hides AS h WHERE h.scope_kind = 'repo' AND h.space_uri = rp.space_uri AND h.did = rp.did AND h.collection = '' AND h.rkey = '' AND h.cid = ''`+
	`)`

// sqlRecordVisible is true when the record in row 'r' (of the records table) is visible — not counting its repo or space.
const sqlRecordVisible string =
	`r.generation = rp.generation` +" "+
	`AND NOT EXISTS (`+
		`SELECT 1 FROM hides AS h WHERE h.scope_kind = 'record' AND h.space_uri = r.space_uri AND h.did = r.did AND h.collection = r.collection AND h.rkey = r.rkey AND (h.cid = '' OR h.cid = r.cid)`+
	`)`
