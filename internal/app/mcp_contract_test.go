package app

import (
	"maps"
	"reflect"
	"testing"

	"github.com/uvwt/agentdock-protocol/mcpcontract"
	"github.com/uvwt/agentdock/internal/config"
)

func TestCanonicalToolDefinitionsMatchSharedContract(t *testing.T) {
	definitions := make(map[string]ToolDefinition, len(mcpcontract.ToolNames()))
	for _, definition := range ToolDefinitions() {
		if mcpcontract.IsCanonicalTool(definition.Name) {
			definitions[definition.Name] = definition
		}
	}
	if len(definitions) != len(mcpcontract.ToolNames()) {
		t.Fatalf("canonical tool count=%d want=%d", len(definitions), len(mcpcontract.ToolNames()))
	}

	for _, name := range mcpcontract.ToolNames() {
		definition, ok := definitions[name]
		if !ok {
			t.Fatalf("canonical tool %s missing", name)
		}
		wantInput, _ := mcpcontract.InputSchema(name)
		spec, _ := toolSpecByName(name)
		base, _ := spec.Contract(name, config.Config{})
		actualInput, actualOutput := base.InputSchema, base.OutputSchema
		if !reflect.DeepEqual(base.InputSchema, definition.InputSchema) {
			t.Fatalf("%s public business contract was altered by execution metadata", name)
		}
		if !reflect.DeepEqual(executionOutputSchema(base.OutputSchema), definition.OutputSchema) {
			t.Fatalf("%s execution output extension drifted", name)
		}
		if name == mcpcontract.ToolAgentDockContext {
			// Standalone AgentDock adds only optional local context fields. Compare
			// every remaining field against the unchanged shared protocol contract.
			actualInput = withoutLocalContextProperty(t, actualInput, "workdir")
			actualOutput = withoutLocalRuntimeRecovery(t, actualOutput)
			actualOutput = withoutLocalContextProperty(t, actualOutput, "instruction_files")
			actualOutput = withoutLocalContextProperty(t, actualOutput, "plugins")
			actualOutput = withoutLocalContextProperty(t, actualOutput, "tasks")
			actualOutput = withoutLocalContextProperty(t, actualOutput, "workspace")
		}
		if !reflect.DeepEqual(actualInput, wantInput) {
			t.Fatalf("%s input schema drifted from shared contract", name)
		}
		var wantOutput map[string]any
		if name == mcpcontract.ToolAgentDockContext {
			wantOutput = mcpcontract.LocalAgentDockContextOutputSchema()
		} else {
			wantOutput, _ = mcpcontract.OutputSchema(name)
		}
		if !reflect.DeepEqual(actualOutput, wantOutput) {
			t.Fatalf("%s output schema drifted from shared contract", name)
		}

		wantAnnotations, _ := mcpcontract.AnnotationContract(name)
		annotations := definition.Annotations
		if annotations == nil ||
			annotations.ReadOnlyHint != wantAnnotations.ReadOnlyHint ||
			!reflect.DeepEqual(annotations.DestructiveHint, wantAnnotations.DestructiveHint) ||
			!reflect.DeepEqual(annotations.OpenWorldHint, wantAnnotations.OpenWorldHint) {
			t.Fatalf("%s annotations drifted: got=%#v want=%#v", name, annotations, wantAnnotations)
		}
		wantIdempotent := wantAnnotations.IdempotentHint != nil && *wantAnnotations.IdempotentHint
		if annotations.IdempotentHint != wantIdempotent {
			t.Fatalf("%s idempotentHint=%v want=%v", name, annotations.IdempotentHint, wantIdempotent)
		}
	}
}

func withoutLocalRuntimeRecovery(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	copy := maps.Clone(schema)
	properties := maps.Clone(schema["properties"].(map[string]any))
	runtimeSchema, _ := properties["runtime"].(map[string]any)
	if runtimeSchema == nil {
		t.Fatal("agentdock_context output is missing runtime")
	}
	runtimeCopy := maps.Clone(runtimeSchema)
	runtimeProperties := maps.Clone(runtimeSchema["properties"].(map[string]any))
	for _, name := range []string{"execution_epoch", "command_recovery"} {
		if runtimeProperties[name] == nil {
			t.Fatalf("local runtime recovery field %q missing", name)
		}
		delete(runtimeProperties, name)
	}
	required, _ := runtimeSchema["required"].([]string)
	filtered := make([]string, 0, len(required))
	for _, name := range required {
		if name == "execution_epoch" || name == "command_recovery" {
			continue
		}
		filtered = append(filtered, name)
	}
	runtimeCopy["properties"] = runtimeProperties
	runtimeCopy["required"] = filtered
	properties["runtime"] = runtimeCopy
	copy["properties"] = properties
	return copy
}

func withoutLocalContextProperty(t *testing.T, schema map[string]any, name string) map[string]any {
	t.Helper()
	copy := maps.Clone(schema)
	properties := maps.Clone(schema["properties"].(map[string]any))
	if properties[name] == nil {
		t.Fatalf("local context extension %q missing", name)
	}
	requiredFields, _ := schema["required"].([]string)
	for _, required := range requiredFields {
		if required == name {
			t.Fatalf("local extension %q must remain optional", name)
		}
	}
	delete(properties, name)
	copy["properties"] = properties
	return copy
}
