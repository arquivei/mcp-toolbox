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

package bigqueryexecutesql

import (
	"strings"
	"testing"
)

// A config with only allowedTables set (no allowedDatasets) must still
// announce the restriction in the sql parameter description. If it doesn't,
// that's a signal the allowlist gate was skipped for a tables-only config.
func TestBuildParamsAllowedTablesInDescription(t *testing.T) {
	params, err := buildParams("blocked", nil, []string{"p.d.t1", "p.d.t2"})
	if err != nil {
		t.Fatalf("buildParams returned error: %s", err)
	}
	desc := params[0].GetDesc()
	for _, want := range []string{"p.d.t1", "p.d.t2"} {
		if !strings.Contains(desc, want) {
			t.Errorf("sql description does not mention allowed table %q; got: %s", want, desc)
		}
	}
}
