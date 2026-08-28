// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bigquerygettableinfo_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bigqueryapi "cloud.google.com/go/bigquery"
	"github.com/google/go-cmp/cmp"
	"github.com/googleapis/mcp-toolbox/internal/server"
	"github.com/googleapis/mcp-toolbox/internal/testutils"
	"github.com/googleapis/mcp-toolbox/internal/tools"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerycommon"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerygettableinfo"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
	"google.golang.org/api/option"
)

func TestParseFromYamlBigQueryGetTableInfo(t *testing.T) {
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
            type: bigquery-get-table-info
            source: my-instance
            description: some description
            `,
			want: server.ToolConfigs{
				"example_tool": bigquerygettableinfo.Config{
					ConfigBase: tools.ConfigBase{
						Name:         "example_tool",
						Description:  "some description",
						AuthRequired: []string{},
					},
					Type:   "bigquery-get-table-info",
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

// TestInvokeAllowedTables verifies that get_table_info uses IsTableAllowed
// (row/data-access gating) rather than the broader IsDatasetVisible: a table
// not itself in AllowedTables must be rejected even if its sibling table in
// the same dataset is allowed.
func TestInvokeAllowedTables(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind": "bigquery#table",
			"id": "p:d.allowed",
			"tableReference": {"projectId": "p", "datasetId": "d", "tableId": "allowed"}
		}`))
	}))
	defer mockServer.Close()

	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("failed to create context with logger: %v", err)
	}

	bqClient, err := bigqueryapi.NewClient(ctx, "p", option.WithEndpoint(mockServer.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("failed to create mocked BigQuery client: %v", err)
	}

	source := &bigquerycommon.MockSource{
		Client:        bqClient,
		AllowedTables: []string{"p.d.allowed"},
	}

	cfg := bigquerygettableinfo.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "get_table_info_tool",
			Description: "Get Table Info",
		},
		Type:   "bigquery-get-table-info",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	getTableInfoTool, ok := tool.(bigquerygettableinfo.Tool)
	if !ok {
		t.Fatalf("expected bigquerygettableinfo.Tool, got %T", tool)
	}

	params, err := getTableInfoTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}

	// The allowed table must succeed (no allowlist error).
	allowedData := map[string]any{"project": "p", "dataset": "d", "table": "allowed"}
	allowedParams, err := parameters.ParseParams(params, allowedData, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}
	if _, err := tool.Invoke(ctx, source, allowedParams, ""); err != nil {
		t.Fatalf("expected allowed table to succeed, got error: %v", err)
	}

	// A sibling table not in the allowlist must be rejected, even though it
	// lives in the same dataset as the allowed table.
	deniedData := map[string]any{"project": "p", "dataset": "d", "table": "secret"}
	deniedParams, err := parameters.ParseParams(params, deniedData, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}
	_, err = tool.Invoke(ctx, source, deniedParams, "")
	if err == nil || !strings.Contains(err.Error(), "access denied to table") {
		t.Fatalf("expected access-denied error for unlisted table, got: %v", err)
	}
}
