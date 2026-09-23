package selfupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/buildinfo"
)

type forbiddenOfflineTransport struct{ calls int }

func (r *forbiddenOfflineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, errors.New("offline policy allowed a network request")
}

func TestOfflinePolicyNeverDownloadsOrApplies(t *testing.T) {
	transport := &forbiddenOfflineTransport{}
	applied := false
	opts := options{
		OfflineOnly: true, CurrentVersion: buildinfo.Version,
		ReleaseAPI: "https://upstream.invalid/releases/latest",
		HTTPClient: &http.Client{Transport: transport},
		Apply: func(context.Context, applyRequest) (applyResult, error) {
			applied = true
			return applyResult{}, nil
		},
	}
	inspection, err := inspectUpdate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	result := inspection.Result
	if result.Code != OfflineUpdateCode || result.Channel != "offline-manual" || result.UpdateAvailable || result.LatestVersion != "" || result.CurrentVersion != normalizeVersion(buildinfo.Version) {
		t.Fatalf("offline response must not claim latest-version knowledge: %+v", result)
	}
	if err := run(context.Background(), opts); !errors.Is(err, ErrOnlineUpdatesDisabled) {
		t.Fatalf("online apply: %v", err)
	}
	if transport.calls != 0 || applied {
		t.Fatalf("offline policy side effects: requests=%d applied=%v", transport.calls, applied)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspectUpdate(cancelled, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled check: %v", err)
	}
	if err := run(cancelled, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled update: %v", err)
	}
}

func TestRuntimeDefaultsToOwnOfflineChannel(t *testing.T) {
	opts, err := runtimeOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.OfflineOnly || opts.ReleaseAPI != "https://api.github.com/repos/eerraa/agentdock/releases/latest" {
		t.Fatalf("unsafe default: %+v", opts)
	}
	result, err := Check(context.Background())
	if err != nil || result.Code != OfflineUpdateCode {
		t.Fatalf("public check: %+v, %v", result, err)
	}
	if err := Run(context.Background(), nil); !errors.Is(err, ErrOnlineUpdatesDisabled) {
		t.Fatalf("public run: %v", err)
	}
	var progress bytes.Buffer
	if err := RunWithProgress(context.Background(), nil, &progress); !errors.Is(err, ErrOnlineUpdatesDisabled) {
		t.Fatalf("public progress run: %v", err)
	}
	decoder := json.NewDecoder(&progress)
	failed := false
	for decoder.More() {
		var event UpdateProgressEvent
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "completed" {
			t.Fatal("disabled update reported success")
		}
		if event.Type == "failed" {
			failed = strings.Contains(event.Error, OfflineUpdateCode)
		}
	}
	if !failed {
		t.Fatal("disabled update omitted terminal failure")
	}
}

func TestDownstreamNumericVersionOrdering(t *testing.T) {
	for _, pair := range [][2]string{{"1.1.0", "1.1.4001"}, {"1.1.4", "1.1.4001"}, {"1.1.4001", "1.1.4002"}} {
		comparison, ok := compareVersions(pair[0], pair[1])
		if !ok || comparison >= 0 {
			t.Fatalf("downstream version ordering: %v => %d, %v", pair, comparison, ok)
		}
	}
}
