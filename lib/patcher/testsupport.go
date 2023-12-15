package patcher

// Make a literal string fit a pointer to string.
func someStr(s string) *string {
	return &s
}
