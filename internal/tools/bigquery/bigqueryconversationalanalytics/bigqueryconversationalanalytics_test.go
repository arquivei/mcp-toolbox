// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bigqueryconversationalanalytics_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/googleapis/mcp-toolbox/internal/server"
	"github.com/googleapis/mcp-toolbox/internal/testutils"
	"github.com/googleapis/mcp-toolbox/internal/tools"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerycommon"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigqueryconversationalanalytics"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
	"golang.org/x/oauth2"
)

// caTestSource wraps bigquerycommon.MockSource to exercise the
// bigquery-conversational-analytics tool's compatibleSource interface, which
// requires a few methods the shared mock does not define (this tool talks to
// the Gemini Data Analytics API directly, not through RetrieveClientAndService).
// It forces client-side OAuth so Invoke never needs a real token source or
// network call before reaching the allowlist gate under test.
type caTestSource struct {
	*bigquerycommon.MockSource
}

func (caTestSource) UseClientAuthorization() bool { return true }

func (caTestSource) BigQueryProject() string      { return "p" }
func (caTestSource) BigQueryLocation() string     { return "us" }
func (caTestSource) BigQueryQuotaProject() string { return "" }
func (caTestSource) GetMaxQueryResultRows() int   { return 100 }
func (caTestSource) BigQueryTokenSourceWithScope(ctx context.Context, scopes []string) (oauth2.TokenSource, error) {
	return nil, nil
}

func TestParseFromYamlBigQueryConversationalAnalytics(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	tcs := []struct {
		desc string
		in   string
		want server.ToolConfigs
	}{
		{
			desc: "basic example",
			in: `
            kind: tool
            name: example_tool
            type: bigquery-conversational-analytics
            source: my-instance
            description: some description
            `,
			want: server.ToolConfigs{
				"example_tool": bigqueryconversationalanalytics.Config{
					ConfigBase: tools.ConfigBase{
						Name:         "example_tool",
						Description:  "some description",
						AuthRequired: []string{},
					},
					Type:   "bigquery-conversational-analytics",
					Source: "my-instance",
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			// Parse contents
			_, _, _, got, _, _, err := server.UnmarshalPrimitiveConfig(ctx, testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("incorrect parse: diff %v", diff)
			}
		})
	}
}

// TestInvokeAllowedTablesValidation verifies the tool checks each referenced
// table via IsTableAllowed (table-level, not just dataset-level), and that a
// table-only allowance is honored even without a full dataset allowance.
func TestInvokeAllowedTablesValidation(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("failed to create context with logger: %v", err)
	}

	source := caTestSource{MockSource: &bigquerycommon.MockSource{
		AllowedTables: []string{"p.d.allowed"},
	}}

	cfg := bigqueryconversationalanalytics.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "conversational_analytics_tool",
			Description: "Conversational Analytics",
		},
		Type:   "bigquery-conversational-analytics",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	caTool, ok := tool.(bigqueryconversationalanalytics.Tool)
	if !ok {
		t.Fatalf("expected bigqueryconversationalanalytics.Tool, got %T", tool)
	}

	params, err := caTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}

	// A table not in the allowlist must be rejected before any network call.
	deniedData := map[string]any{
		"user_query_with_context": "how many rows",
		"table_references":        `[{"projectId":"p","datasetId":"d","tableId":"secret"}]`,
	}
	deniedParams, err := parameters.ParseParams(params, deniedData, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}
	_, err = tool.Invoke(ctx, source, deniedParams, "Bearer test-token")
	if err == nil || !strings.Contains(err.Error(), "is not allowed") {
		t.Fatalf("expected access-denied error for unlisted table, got: %v", err)
	}
}

// TestGetParametersMentionsAllowedTablesOnlyConfig proves that a
// tables-only allowlist (no AllowedDatasets) is still surfaced in the
// `table_references` parameter description. Invoke enforces AllowedTables
// via IsTableAllowed regardless of AllowedDatasets, but before this fix the
// description only branched on len(allowedDatasets) > 0, so an agent reading
// the tool's own documented interface would see no restriction at all.
func TestGetParametersMentionsAllowedTablesOnlyConfig(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("failed to create context with logger: %v", err)
	}
	source := caTestSource{MockSource: &bigquerycommon.MockSource{
		AllowedTables: []string{"p.d.allowed"},
	}}
	cfg := bigqueryconversationalanalytics.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "conversational_analytics_tool",
			Description: "Conversational Analytics",
		},
		Type:   "bigquery-conversational-analytics",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	caTool, ok := tool.(bigqueryconversationalanalytics.Tool)
	if !ok {
		t.Fatalf("expected bigqueryconversationalanalytics.Tool, got %T", tool)
	}
	params, err := caTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}
	var tableRefsDesc string
	found := false
	for _, p := range params {
		if p.GetName() == "table_references" {
			tableRefsDesc = p.GetDesc()
			found = true
		}
	}
	if !found {
		t.Fatalf("table_references parameter not found")
	}
	if !strings.Contains(tableRefsDesc, "p.d.allowed") {
		t.Errorf("table_references description does not mention allowed table 'p.d.allowed' from a tables-only config; got: %s", tableRefsDesc)
	}
}
