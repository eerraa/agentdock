package buildinfo

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestDownstreamIdentityAndCompleteSourceSHA(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })
	Commit = "0123456789abcdef0123456789abcdef01234567"
	info := Current()
	if info.SourceCommit != Commit || info.Commit != Commit[:12] {
		t.Fatalf("source identity lost: %+v", info)
	}
	if info.Distribution != "eerraa" || info.UpstreamVersion != "1.1.5" || info.DownstreamRevision != 1 {
		t.Fatalf("downstream identity: %+v", info)
	}
	if _, err := json.Marshal(info); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(Version, ".")
	if len(parts) != 3 {
		t.Fatalf("non-numeric product version: %s", Version)
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 65535 {
			t.Fatalf("invalid Windows version component: %s", part)
		}
	}
	patch, _ := strconv.Atoi(parts[2])
	if patch != 5*1000+DownstreamRevision {
		t.Fatalf("downstream revision encoding: %s", Version)
	}
}
