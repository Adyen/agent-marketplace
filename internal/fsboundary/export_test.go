package fsboundary

func ReplaceOpenatForTest(open func(int, string, int, uint32) (int, error)) func() {
	previous := openat
	openat = open
	return func() { openat = previous }
}
