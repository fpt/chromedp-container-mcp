package tool

import (
	"strings"
	"testing"
)

func TestAnnotateDepthTruncation(t *testing.T) {
	// No marker -> returned unchanged.
	clean := "<body><h1>Hello</h1></body>"
	if got := annotateDepthTruncation(clean, 5); got != clean {
		t.Errorf("expected unchanged output, got %q", got)
	}

	// Each known marker should trigger a warning that mentions the depth.
	for _, marker := range truncationMarkers {
		out := "<body>" + marker + "</body>"
		got := annotateDepthTruncation(out, 2)
		if !strings.HasPrefix(got, "⚠") {
			t.Errorf("marker %q: expected a leading warning, got %q", marker, got)
		}
		if !strings.Contains(got, "depth=2") {
			t.Errorf("marker %q: warning should mention depth=2, got %q", marker, got)
		}
		if !strings.Contains(got, out) {
			t.Errorf("marker %q: original output should be preserved", marker)
		}
	}
}
