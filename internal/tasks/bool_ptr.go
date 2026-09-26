package tasks

// boolPtr returns a pointer to b. It is used when building task structs whose
// optional bool fields are declared as *bool so an explicit false is
// distinguishable from an omitted key.
func boolPtr(b bool) *bool { return &b }

// boolValue dereferences p, returning def when p is nil. Optional bool task
// fields are declared as *bool so `restart: false` (and friends) survive
// decoding: go-defaults leaves pointer fields untouched, so the `default:` tag
// only drives the generated docs table and the real default is applied here.
func boolValue(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
