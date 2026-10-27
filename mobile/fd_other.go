//go:build !unix

package mobile

// closeFD: the library only runs on Android; this keeps go vet ./... working
// on Windows.
func closeFD(int) {}
