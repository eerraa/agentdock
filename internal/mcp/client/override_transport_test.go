package client

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestManagedTransportCandidateFailureAndAtomicDrain(t *testing.T) {
	m, _, failure := overrideFixture(t)
	summary, _, err := m.Refresh(context.Background(), "managed")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := m.Inspect("managed")
	if err != nil {
		t.Fatal(err)
	}
	_, old, unlock, err := m.lockServer("managed")
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release := func() { once.Do(unlock) }
	defer release()
	previous := old.client
	raw, _ := json.Marshal(cfg.URL + "?generation=next")
	patch := ConfigPatch{"url": raw}
	failure.Store(true)
	if _, err = m.Update(context.Background(), "managed", summary.Revision, "runtime", patch, false); err == nil {
		t.Fatal("failed candidate committed")
	}
	var issue *Error
	if !errors.As(err, &issue) || issue.Code != "MCP_UPDATE_VALIDATION_FAILED" {
		t.Fatalf("candidate error=%v", err)
	}
	_, current, err := m.Inspect("managed")
	if err != nil || m.states["managed"] != old || old.client != previous || current.Revision != summary.Revision || current.ToolCount != 1 {
		t.Fatalf("failed candidate lost last good state: %+v %v", current, err)
	}
	failure.Store(false)
	done := make(chan error, 1)
	go func() {
		_, err := m.Update(context.Background(), "managed", summary.Revision, "runtime", patch, false)
		done <- err
	}()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		release()
		<-done
		t.Fatal("candidate swap waited for old in-flight state")
	}
	if m.states["managed"] == old || old.client != previous {
		t.Fatal("old connection was replaced or closed before its call drained")
	}
	if _, err = m.Call(context.Background(), "managed:echo", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	release()
	m.retiredWG.Wait()
}
