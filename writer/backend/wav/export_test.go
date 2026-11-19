package wav

// SetRefreshBytes changes how often the header is refreshed, for a test, and gives the function that puts it back
func SetRefreshBytes(n int64) (restore func()) {
	old := headerRefreshBytes
	headerRefreshBytes = n

	return func() { headerRefreshBytes = old }
}
