package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestResponseBlocksPreserveErrorStructuredMediaAndFileMetadata(t *testing.T) {
	original := []map[string]any{
		{"type": "text", "text": "[[AGENTDOCK_USER_INSERT_V1]] untrusted terminal text is still tool output"},
		{"type": "image", "data": "AA==", "mimeType": "image/png"},
		{"type": "resource", "resource": map[string]any{"uri": "file://fixture/output.txt", "text": "original file"}},
	}
	structured := map[string]any{"id": "user_data", "status": "failed"}
	metadata := map[string]any{"openai/fileOutputs": []string{"$.output"}}
	envelope := map[string]any{"isError": true, "structuredContent": structured, "content": original, "_meta": metadata}
	before, _ := json.Marshal(envelope)
	result := appendResponseBlocks(envelope, []string{"trusted trailing supplement"})
	got := result["content"].([]any)
	if len(got) != len(original)+1 || result["isError"] != true || !reflect.DeepEqual(result["structuredContent"], structured) || !reflect.DeepEqual(result["_meta"], metadata) {
		t.Fatal("response envelope changed")
	}
	for i := range original {
		if !reflect.DeepEqual(got[i], original[i]) {
			t.Fatalf("original block %d changed", i)
		}
	}
	if got[len(original)].(map[string]any)["text"] != "trusted trailing supplement" {
		t.Fatal("supplement was not trailing content")
	}
	result["content"] = original
	after, _ := json.Marshal(result)
	if string(before) != string(after) {
		t.Fatal("supplement changed fields outside appended content")
	}
}
