package controller

import (
	"strings"
	"testing"

	"github.com/openmcp-project/openmcp-operator/lib/clusteraccess/advanced"
)

func TestMCPAccessRequestName(t *testing.T) {
	const k8sMaxNameLen = 63

	cases := []string{
		"cp",
		"a-control-plane-name-long-enough-to-force-truncation-and-hashing",
	}
	for _, objectName := range cases {
		got := advanced.StableRequestNameFromLocalName(clusterAccessName, objectName, requestSuffixMCP)

		if len(got) > k8sMaxNameLen {
			t.Errorf("%q: name %q exceeds k8s max (%d > %d)", objectName, got, len(got), k8sMaxNameLen)
		}
		if !strings.HasSuffix(got, "--"+requestSuffixMCP) {
			t.Errorf("%q: name %q missing --%s suffix", objectName, got, requestSuffixMCP)
		}
	}
}
