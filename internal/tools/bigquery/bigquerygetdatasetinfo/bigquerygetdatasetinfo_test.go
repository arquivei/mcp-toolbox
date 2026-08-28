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

package bigquerygetdatasetinfo_test

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
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerygetdatasetinfo"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
	"google.golang.org/api/option"
)

func TestParseFromYamlBigQueryGetDatasetInfo(t *testing.T) {
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
            type: bigquery-get-dataset-info
            source: my-instance
            description: some description
            `,
			want: server.ToolConfigs{
				"example_tool": bigquerygetdatasetinfo.Config{
					ConfigBase: tools.ConfigBase{
						Name:         "example_tool",
						Description:  "some description",
						AuthRequired: []string{},
					},
					Type:   "bigquery-get-dataset-info",
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

// TestInvokeDatasetVisibleViaTableEntry verifies get_dataset_info uses
// IsDatasetVisible: a dataset that has no full dataset allowance but holds an
// allowed table must still be visible (dataset/table structure, not rows), and
// a dataset with no allowed tables at all must remain denied.
func TestInvokeDatasetVisibleViaTableEntry(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"kind": "bigquery#dataset",
			"id": "p:d",
			"datasetReference": {"projectId": "p", "datasetId": "d"}
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

	cfg := bigquerygetdatasetinfo.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "get_dataset_info_tool",
			Description: "Get Dataset Info",
		},
		Type:   "bigquery-get-dataset-info",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	getDatasetInfoTool, ok := tool.(bigquerygetdatasetinfo.Tool)
	if !ok {
		t.Fatalf("expected bigquerygetdatasetinfo.Tool, got %T", tool)
	}

	params, err := getDatasetInfoTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}

	// Dataset "d" holds an allowed table, so it must be visible even without a
	// full dataset allowance.
	visibleData := map[string]any{"project": "p", "dataset": "d"}
	visibleParams, err := parameters.ParseParams(params, visibleData, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}
	if _, err := tool.Invoke(ctx, source, visibleParams, ""); err != nil {
		t.Fatalf("expected dataset reachable via table entry to be visible, got error: %v", err)
	}

	// Dataset "other" has no allowed table and no dataset allowance: must be denied.
	hiddenData := map[string]any{"project": "p", "dataset": "other"}
	hiddenParams, err := parameters.ParseParams(params, hiddenData, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}
	_, err = tool.Invoke(ctx, source, hiddenParams, "")
	if err == nil || !strings.Contains(err.Error(), "access denied to dataset") {
		t.Fatalf("expected access-denied error for hidden dataset, got: %v", err)
	}
}
