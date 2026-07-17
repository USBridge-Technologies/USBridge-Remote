package clipboard

// Backend is native system-clipboard access for one platform. Read/Write
// operate on whichever representation (text/image/file) is currently on the
// clipboard; ChangeStamp is polled frequently so it must be cheap — it
// should never perform a full content read.
type Backend interface {
	// ChangeStamp returns a token that changes whenever the system
	// clipboard's content changes. Implementations without a native
	// sequence counter may hash a cheap summary instead (e.g. a Linux
	// TARGETS listing) rather than reading the full payload.
	ChangeStamp() (string, error)

	// Read returns the current clipboard content. ok=false means the
	// clipboard holds nothing, or a format this backend doesn't understand.
	Read() (content Content, ok bool, err error)

	// Write sets the system clipboard to content.
	Write(content Content) error
}

// sanitizeFileName strips path separators from a peer-supplied file name so
// it can't escape the temp directory files are materialized into before
// being registered on the native clipboard.
func sanitizeFileName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch r {
		case '/', '\\', 0:
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "file"
	}
	return string(out)
}
