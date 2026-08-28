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

package bigquerylistdatasetids_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/googleapis/mcp-toolbox/internal/server"
	"github.com/googleapis/mcp-toolbox/internal/testutils"
	"github.com/googleapis/mcp-toolbox/internal/tools"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerycommon"
	"github.com/googleapis/mcp-toolbox/internal/tools/bigquery/bigquerylistdatasetids"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
)

func TestParseFromYamlBigQueryListDatasetIds(t *testing.T) {
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
            type: bigquery-list-dataset-ids
            source: my-instance
            description: some description
            `,
			want: server.ToolConfigs{
				"example_tool": bigquerylistdatasetids.Config{
					ConfigBase: tools.ConfigBase{
						Name:         "example_tool",
						Description:  "some description",
						AuthRequired: []string{},
					},
					Type:   "bigquery-list-dataset-ids",
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

// TestInvokeSurfacesDatasetsFromTableEntries verifies list_dataset_ids derives
// dataset names from table-level allowlist entries (in addition to full
// dataset allowances) so the agent can discover a dataset it only has a
// table-level allowance into.
func TestInvokeSurfacesDatasetsFromTableEntries(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("failed to create context with logger: %v", err)
	}

	source := &bigquerycommon.MockSource{
		AllowedDatasets: []string{"p.ds_full"},
		AllowedTables:   []string{"p.ds_tables_only.t1", "p.ds_tables_only.t2", "p.ds_full.t3"},
	}

	cfg := bigquerylistdatasetids.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "list_dataset_ids_tool",
			Description: "List Dataset Ids",
		},
		Type:   "bigquery-list-dataset-ids",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	listDatasetIdsTool, ok := tool.(bigquerylistdatasetids.Tool)
	if !ok {
		t.Fatalf("expected bigquerylistdatasetids.Tool, got %T", tool)
	}

	params, err := listDatasetIdsTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}
	paramVals, err := parameters.ParseParams(params, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error parsing parameters: %v", err)
	}

	result, err := tool.Invoke(ctx, source, paramVals, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"p.ds_full", "p.ds_tables_only"}
	if diff := cmp.Diff(want, result); diff != "" {
		t.Fatalf("incorrect dataset listing: diff %v", diff)
	}
}

// TestGetParametersDescribesProjectAsIgnoredForTablesOnlyConfig proves that a
// tables-only allowlist (no AllowedDatasets) still gets an accurate `project`
// parameter description. Invoke ignores the `project` parameter entirely
// whenever either AllowedDatasets or AllowedTables restrict the tool (see
// the `len(source.BigQueryAllowedDatasets()) > 0 || len(source.BigQueryAllowedTables()) > 0`
// branch), but before this fix the description only branched on
// len(allowedDatasets) > 0, so a tables-only config left the description
// reading "The Google Cloud project to list dataset ids." — actively
// misleading, since the value is actually discarded.
func TestGetParametersDescribesProjectAsIgnoredForTablesOnlyConfig(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("failed to create context with logger: %v", err)
	}
	source := &bigquerycommon.MockSource{
		AllowedTables: []string{"p.d.t1"},
	}
	cfg := bigquerylistdatasetids.Config{
		ConfigBase: tools.ConfigBase{
			Name:        "list_dataset_ids_tool",
			Description: "List Dataset Ids",
		},
		Type:   "bigquery-list-dataset-ids",
		Source: "my-bq-source",
	}
	tool, err := cfg.Initialize(ctx)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}
	listDatasetIdsTool, ok := tool.(bigquerylistdatasetids.Tool)
	if !ok {
		t.Fatalf("expected bigquerylistdatasetids.Tool, got %T", tool)
	}
	params, err := listDatasetIdsTool.GetParameters(source)
	if err != nil {
		t.Fatalf("failed to get parameters: %v", err)
	}
	var projectDesc string
	found := false
	for _, p := range params {
		if p.GetName() == "project" {
			projectDesc = p.GetDesc()
			found = true
		}
	}
	if !found {
		t.Fatalf("project parameter not found")
	}
	if projectDesc == "The Google Cloud project to list dataset ids." {
		t.Errorf("project description is unchanged from the unrestricted default even though a tables-only allowlist makes it effectively ignored; got: %s", projectDesc)
	}
	if !strings.Contains(projectDesc, "ignored") {
		t.Errorf("expected project description to indicate the parameter is ignored under a tables-only allowlist; got: %s", projectDesc)
	}
}
