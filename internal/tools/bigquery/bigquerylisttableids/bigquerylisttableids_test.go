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

package bigquerylisttableids_test

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
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerylisttableids"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
	"google.golang.org/api/option"
)

// TestListTableIds_FiltersToAllowedTables is a regression test for the
// underlying access decisions used by list_table_ids: a dataset that is
// reachable only via a table-level allowance must be visible (structure), but
// must not leak sibling tables it does not have an allowance for.
func TestListTableIds_FiltersToAllowedTables(t *testing.T) {
	source := &bigquerycommon.MockSource{
		AllowedTables: []string{"p.d.allowed"},
	}
	if !source.IsDatasetVisible("p", "d") {
		t.Fatal("dataset with an allowed table must be visible to list_table_ids")
	}
	if source.IsTableAllowed("p", "d", "secret") {
		t.Fatal("table not in the allowlist must not survive the listing filter")
	}
	if !source.IsTableAllowed("p", "d", "allowed") {
		t.Fatal("allowed table must survive the listing filter")
	}
}

func TestParseFromYamlBigQueryListTableIds(t *testing.T) {
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
            type: bigquery-list-table-ids
            source: my-instance
            description: some description
            `,
			want: server.ToolConfigs{
				"example_tool": bigquerylisttableids.Config{
					ConfigBase: tools.ConfigBase{
						Name:         "example_tool",
						Description:  "some description",
						AuthRequired: []string{},
					},
					Type:   "bigquery-list-table-ids",
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

// TestInvokeFiltersListingToAllowedTables proves the tool-level security
// boundary: a dataset reachable only via one table-level allowance must not
// leak its other sibling tables in the listing, and the filter must apply
// after quote-stripping normalization (the map keys are unquoted).
func TestInvokeFiltersListingToAllowedTables(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind": "bigquery#tableList",
			"tables": [
				{"kind": "bigquery#table", "id": "p:d.allowed", "tableReference": {"projectId": "p", "datasetId": "d", "tableId": "\"allowed\""}},
				{"kind": "bigquery#table", "id": "p:d.secret", "tableReference": {"projectId": "p", "datasetId": "d", "tableId": "secret"}},
				{"kind": "bigquery#table", "id": "p:d.secret2", "tableReference": {"projectId": "p", "datasetId": "d", "tableId": "secret2"}}
			]
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

	cfg := bigquerylisttableids.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "list_table_ids_tool",
			Description: "List Table Ids",
		},
		Type:   "bigquery-list-table-ids",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	listTableIdsTool, ok := tool.(bigquerylisttableids.Tool)
	if !ok {
		t.Fatalf("expected bigquerylisttableids.Tool, got %T", tool)
	}

	params, err := listTableIdsTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}
	data := map[string]any{"project": "p", "dataset": "d"}
	paramVals, err := parameters.ParseParams(params, data, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}

	result, err := tool.Invoke(ctx, source, paramVals, "")
	if err != nil {
		t.Fatalf("expected dataset reachable via table entry to be listable, got error: %v", err)
	}

	got, ok := result.([]any)
	if !ok {
		t.Fatalf("expected []any result, got %T", result)
	}
	if len(got) != 1 || !strings.Contains(got[0].(string), "allowed") {
		t.Fatalf("expected listing to contain only the allowed table, got: %v", got)
	}
}
