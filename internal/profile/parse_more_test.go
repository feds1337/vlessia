package profile

import (
	"strings"
	"testing"
)

func TestOpenVPNFileReferences(t *testing.T) {
	for _, line := range []string{"ca ca.pem", "\ufeffkey key.pem", "auth-user-pass credentials", "\"cert\" cert.pem"} {
		if err := checkOpenVPNFileReferences(line); err == nil {
			t.Fatalf("accepted external reference %q", line)
		}
	}
	embedded := strings.Join([]string{"<ca> # cert", "ca inside certificate", "</ca>", "auth-user-pass # ask in the UI"}, "\n")
	if err := checkOpenVPNFileReferences(embedded); err != nil {
		t.Fatal(err)
	}
}
