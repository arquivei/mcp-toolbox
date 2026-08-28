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

package bigquerycommon

import (
	"strings"
	"testing"
)

func TestInitializeDatasetParameters_MentionsAllowedTables(t *testing.T) {
	// Config with allowedTables only: the agent needs to know which dataset
	// it can call get_table_info against, otherwise the allowed table is
	// unreachable in practice.
	_, datasetParam := InitializeDatasetParameters(
		nil,
		[]string{"p.d.t1", "p.d.t2"},
		"p",
		"project", "dataset",
		"The project.", "The dataset.",
	)
	desc := datasetParam.GetDesc()
	if !strings.Contains(desc, "d") {
		t.Errorf("dataset description does not mention dataset `d`; got: %s", desc)
	}
	for _, want := range []string{"t1", "t2"} {
		if !strings.Contains(desc, want) {
			t.Errorf("dataset description does not mention allowed table %q; got: %s", want, desc)
		}
	}
}

func TestInitializeDatasetParameters_SingleDatasetStillWorks(t *testing.T) {
	// Regression: the single-dataset path does parts[1] without checking
	// length. A 3-part entry must never reach it.
	_, datasetParam := InitializeDatasetParameters(
		[]string{"p.only_dataset"},
		nil,
		"p",
		"project", "dataset",
		"The project.", "The dataset.",
	)
	if !strings.Contains(datasetParam.GetDesc(), "only_dataset") {
		t.Errorf("expected description to name the single allowed dataset; got: %s", datasetParam.GetDesc())
	}
}

func TestInitializeDatasetParameters_SingleDatasetKeepsDefault(t *testing.T) {
	// Regression: when allowedDatasets has exactly one entry, the existing
	// code path attaches a WithStringDefault to datasetParam. Adding
	// allowedTables handling must not silently drop that default.
	_, datasetParam := InitializeDatasetParameters(
		[]string{"p.only_dataset"},
		[]string{"p.other_dataset.t1"},
		"p",
		"project", "dataset",
		"The project.", "The dataset.",
	)
	if got := datasetParam.GetDefault(); got != "only_dataset" {
		t.Errorf("expected dataset default to remain %q, got %q", "only_dataset", got)
	}
}

func TestInitializeDatasetParameters_MixedProjectDoesNotOverclaimSingleProject(t *testing.T) {
	// Bug: allowedDatasets has exactly one entry (projA.ds1), but
	// allowedTables references a table in a different project (projB). The
	// old code unconditionally treated len(allowedDatasets) == 1 as "there is
	// only one allowed project" and wrote "Must be `projA`." into the project
	// description while also defaulting the project param to `projA`. That
	// makes the projB table effectively unreachable via the tool's own
	// documented interface, even though enforcement doesn't care what
	// project value is actually passed.
	projectParam, _ := InitializeDatasetParameters(
		[]string{"projA.ds1"},
		[]string{"projB.ds2.t1"},
		"callerDefault",
		"project", "dataset",
		"The project.", "The dataset.",
	)
	desc := projectParam.GetDesc()
	if strings.Contains(desc, "Must be `projA`") {
		t.Errorf("project description wrongly claims a single required project when projB is also allowed via allowedTables; got: %s", desc)
	}
	if !strings.Contains(desc, "projA") || !strings.Contains(desc, "projB") {
		t.Errorf("project description should mention both allowed projects; got: %s", desc)
	}
	// Since more than one project is actually allowed, the fix must not force
	// the default to either projA or projB — it should preserve whatever
	// default the caller supplied (e.g. the source's own default project).
	if got := projectParam.GetDefault(); got != "callerDefault" {
		t.Errorf("project default should remain the caller-supplied default when multiple projects are allowed; got: %v", got)
	}
}

func TestInitializeDatasetParameters_SingleProjectAcrossDatasetsAndTablesStillRestricts(t *testing.T) {
	// When allowedDatasets and allowedTables agree on a single project, the
	// restrictive "Must be `X`." description and default should still apply.
	projectParam, _ := InitializeDatasetParameters(
		[]string{"p.ds1"},
		[]string{"p.ds2.t1"},
		"p",
		"project", "dataset",
		"The project.", "The dataset.",
	)
	desc := projectParam.GetDesc()
	if !strings.Contains(desc, "Must be `p`") {
		t.Errorf("expected project description to still restrict to a single project `p`; got: %s", desc)
	}
	if got := projectParam.GetDefault(); got != "p" {
		t.Errorf("expected project default to be %q, got %v", "p", got)
	}
}
