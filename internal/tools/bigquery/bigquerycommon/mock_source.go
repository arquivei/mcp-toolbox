// Copyright 2026 Google LLC
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
	"context"
	"fmt"
	"strings"

	bigqueryapi "cloud.google.com/go/bigquery"
	"github.com/googleapis/mcp-toolbox/internal/sources"
	bigqueryds "github.com/googleapis/mcp-toolbox/internal/sources/bigquery"
	"github.com/googleapis/mcp-toolbox/internal/tools"
	bigqueryrestapi "google.golang.org/api/bigquery/v2"
)

// MockSource is a reusable mock implementation of sources.Source for BigQuery tool tests.
type MockSource struct {
	sources.Source
	CalledSQL          string
	Client             *bigqueryapi.Client
	Service            *bigqueryrestapi.Service
	AllowedDatasets    []string
	AllowedTables      []string
	RunSQLResult       any
	RunSQLError        error
	Project            string
	Location           string
	QuotaProject       string
	MaxQueryResultRows int
}

func (m *MockSource) BigQueryProject() string {
	return m.Project
}

func (m *MockSource) BigQueryLocation() string {
	return m.Location
}

func (m *MockSource) BigQueryQuotaProject() string {
	return m.QuotaProject
}

func (m *MockSource) GetMaxQueryResultRows() int {
	return m.MaxQueryResultRows
}

func (m *MockSource) BigQueryClient() *bigqueryapi.Client {
	return m.Client
}

func (m *MockSource) UseClientAuthorization() bool {
	return false
}

func (m *MockSource) BigQueryWriteMode() string {
	return "allowed"
}

func (m *MockSource) GetAuthTokenHeaderName() string {
	return ""
}

func (m *MockSource) GetMaximumBytesBilled() int64 {
	return 0
}

func (m *MockSource) IsDatasetAllowed(projectID, datasetID string) bool {
	if len(m.AllowedDatasets) == 0 {
		return true
	}
	target := fmt.Sprintf("%s.%s", projectID, datasetID)
	for _, allowed := range m.AllowedDatasets {
		if allowed == target || allowed == datasetID {
			return true
		}
	}
	return false
}

func (m *MockSource) BigQueryAllowedDatasets() []string {
	return m.AllowedDatasets
}

func (m *MockSource) BigQueryAllowedTables() []string {
	return m.AllowedTables
}

// IsTableAllowed reports whether a specific table may be accessed. Mirrors the
// logic in bigquery.Source.IsTableAllowed for use in tool unit tests.
func (m *MockSource) IsTableAllowed(projectID, datasetID, tableID string) bool {
	if len(m.AllowedDatasets) == 0 && len(m.AllowedTables) == 0 {
		return true
	}
	// IsDatasetAllowed returns true when AllowedDatasets is empty (meaning "no
	// restriction" for that check alone), which would incorrectly allow every
	// dataset for a tables-only configuration. Only delegate to it when
	// AllowedDatasets actually has entries.
	if len(m.AllowedDatasets) > 0 && m.IsDatasetAllowed(projectID, datasetID) {
		return true
	}
	if strings.Contains(tableID, "*") {
		return false
	}
	target := fmt.Sprintf("%s.%s.%s", projectID, datasetID, tableID)
	for _, allowed := range m.AllowedTables {
		if allowed == target {
			return true
		}
	}
	return false
}

// IsDatasetVisible reports whether a dataset may be introspected at all: either
// it is allowed outright, or it holds at least one allowed table. Mirrors
// bigquery.Source.IsDatasetVisible for use in tool unit tests.
func (m *MockSource) IsDatasetVisible(projectID, datasetID string) bool {
	if len(m.AllowedDatasets) == 0 && len(m.AllowedTables) == 0 {
		return true
	}
	if len(m.AllowedDatasets) > 0 && m.IsDatasetAllowed(projectID, datasetID) {
		return true
	}
	prefix := fmt.Sprintf("%s.%s.", projectID, datasetID)
	for _, allowed := range m.AllowedTables {
		if strings.HasPrefix(allowed, prefix) {
			return true
		}
	}
	return false
}

func (m *MockSource) BigQuerySession() bigqueryds.BigQuerySessionProvider {
	return func(ctx context.Context) (*bigqueryds.Session, error) {
		return &bigqueryds.Session{ID: "mock-session-id"}, nil
	}
}

func (m *MockSource) RetrieveClientAndService(tools.AccessToken) (*bigqueryapi.Client, *bigqueryrestapi.Service, error) {
	return m.Client, m.Service, nil
}

func (m *MockSource) RunSQL(ctx context.Context, client *bigqueryapi.Client, sql string, queryType string, params []bigqueryapi.QueryParameter, connProps []*bigqueryapi.ConnectionProperty, labels map[string]string) (any, error) {
	m.CalledSQL = sql
	return m.RunSQLResult, m.RunSQLError
}
