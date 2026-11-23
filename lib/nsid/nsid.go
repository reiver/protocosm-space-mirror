package libnsid

// NSIDPrefix is the namespace for this space-mirror's own XRPC end-points.
const NSIDPrefix string = "link.reiver.atproto.spacemirror"

// NSIDPrefixRepo is the namespace for the repo-host-style read end-points
// (getRecord, listRecords, getBlob, listBlobs).
//
// NOT RECOMMENDED: changing this to "com.atproto.space" serves official method names
// without the space-credential checks and MACs those names imply.
const NSIDPrefixRepo string = NSIDPrefix
