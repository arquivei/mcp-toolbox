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
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	bigqueryapi "cloud.google.com/go/bigquery"
	"github.com/googleapis/mcp-toolbox/internal/util/parameters"
	bigqueryrestapi "google.golang.org/api/bigquery/v2"
)

// validBQTableID matches BigQuery table identifiers in 'dataset.table' or
// 'project.dataset.table' form. Components are restricted to letters, digits,
// and underscores — the character set that BigQuery allows for dataset and
// table IDs and that is safe to interpolate inside a backtick-quoted SQL
// identifier.
var validBQTableID = regexp.MustCompile(`^[a-zA-Z0-9_-]+(\.([a-zA-Z0-9_]+)){1,2}$`)

// validBQColumnName matches BigQuery column names: a letter or underscore
// followed by letters, digits, or underscores.
var validBQColumnName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidTableID returns true if s is a safe BigQuery table identifier of the
// form 'dataset.table' or 'project.dataset.table'. Values that fail this check
// must not be interpolated into backtick-quoted SQL.
func ValidTableID(s string) bool {
	return validBQTableID.MatchString(s)
}

// ValidColumnName returns true if s is a safe BigQuery column name.
// Values that fail this check must not be interpolated as SQL identifiers
// or into single-quoted SQL string arguments that represent column references.
func ValidColumnName(s string) bool {
	return validBQColumnName.MatchString(s)
}

// DryRunQuery performs a dry run of the SQL query to validate it and get metadata.
func DryRunQuery(ctx context.Context, restService *bigqueryrestapi.Service, projectID string, location string, sql string, params []*bigqueryrestapi.QueryParameter, connProps []*bigqueryapi.ConnectionProperty, maximumBytesBilled int64) (*bigqueryrestapi.Job, error) {
	useLegacySql := false

	restConnProps := make([]*bigqueryrestapi.ConnectionProperty, len(connProps))
	for i, prop := range connProps {
		restConnProps[i] = &bigqueryrestapi.ConnectionProperty{Key: prop.Key, Value: prop.Value}
	}

	jobToInsert := &bigqueryrestapi.Job{
		JobReference: &bigqueryrestapi.JobReference{
			ProjectId: projectID,
			Location:  location,
		},
		Configuration: &bigqueryrestapi.JobConfiguration{
			DryRun: true,
			Query: &bigqueryrestapi.JobConfigurationQuery{
				Query:                sql,
				UseLegacySql:         &useLegacySql,
				ConnectionProperties: restConnProps,
				QueryParameters:      params,
				MaximumBytesBilled:   maximumBytesBilled,
			},
		},
	}

	insertResponse, err := restService.Jobs.Insert(projectID, jobToInsert).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to insert dry run job: %w", err)
	}
	return insertResponse, nil
}

// BQTypeStringFromToolType converts a tool parameter type string to a BigQuery standard SQL type string.
func BQTypeStringFromToolType(toolType string) (string, error) {
	switch toolType {
	case parameters.TypeString:
		return "STRING", nil
	case parameters.TypeInt:
		return "INT64", nil
	case parameters.TypeFloat:
		return "FLOAT64", nil
	case parameters.TypeBool:
		return "BOOL", nil
	case parameters.TypeMap:
		return "STRUCT", nil
	default:
		return "", fmt.Errorf("unsupported tool parameter type for BigQuery: %s", toolType)
	}
}

// InitializeDatasetParameters generates project and dataset tool parameters
// based on allowedDatasets and allowedTables. allowedTables surfaces
// individually-allowed tables (in `project.dataset.table` form) in the
// dataset parameter's description, so an agent that only has table-level
// access can still discover which dataset value to pass to
// get_table_info/get_dataset_info/list_table_ids.
func InitializeDatasetParameters(
	allowedDatasets []string,
	allowedTables []string,
	defaultProjectID string,
	projectKey, datasetKey string,
	projectDescription, datasetDescription string,
) (projectParam, datasetParam parameters.Parameter) {
	// singleDatasetDefault holds the default value that the single-dataset
	// branch below attaches to datasetParam via WithStringDefault. If the
	// allowedTables block later rebuilds datasetParam, it must reapply this
	// default explicitly, or the default silently disappears.
	var singleDatasetDefault string
	var hasSingleDatasetDefault bool

	if len(allowedDatasets) > 0 {
		if len(allowedDatasets) == 1 {
			parts := strings.Split(allowedDatasets[0], ".")
			datasetID := parts[1]
			datasetDescription += fmt.Sprintf(" Must be `%s`.", datasetID)
			singleDatasetDefault = datasetID
			hasSingleDatasetDefault = true
			datasetParam = parameters.NewStringParameter(datasetKey, datasetDescription, parameters.WithStringDefault(datasetID))
		} else {
			datasetIDsByProject := make(map[string][]string)
			for _, ds := range allowedDatasets {
				parts := strings.Split(ds, ".")
				project := parts[0]
				dataset := parts[1]
				datasetIDsByProject[project] = append(datasetIDsByProject[project], fmt.Sprintf("`%s`", dataset))
			}

			var datasetDescriptions []string
			for project, datasets := range datasetIDsByProject {
				sort.Strings(datasets)
				datasetList := strings.Join(datasets, ", ")
				datasetDescriptions = append(datasetDescriptions, fmt.Sprintf("%s from project `%s`", datasetList, project))
			}
			sort.Strings(datasetDescriptions)
			datasetDescription += fmt.Sprintf(" Must be one of the allowed datasets: %s.", strings.Join(datasetDescriptions, "; "))
			datasetParam = parameters.NewStringParameter(datasetKey, datasetDescription)
		}
	} else {
		datasetParam = parameters.NewStringParameter(datasetKey, datasetDescription)
	}

	if len(allowedTables) > 0 {
		tablesByDataset := make(map[string][]string)
		for _, t := range allowedTables {
			parts := strings.Split(t, ".")
			if len(parts) != 3 {
				continue
			}
			ds := fmt.Sprintf("%s.%s", parts[0], parts[1])
			tablesByDataset[ds] = append(tablesByDataset[ds], fmt.Sprintf("`%s`", parts[2]))
		}
		datasetKeys := make([]string, 0, len(tablesByDataset))
		for ds := range tablesByDataset {
			datasetKeys = append(datasetKeys, ds)
		}
		sort.Strings(datasetKeys)

		var fragments []string
		for _, ds := range datasetKeys {
			sort.Strings(tablesByDataset[ds])
			fragments = append(fragments, fmt.Sprintf("%s from dataset `%s`",
				strings.Join(tablesByDataset[ds], ", "), ds))
		}
		datasetDescription += fmt.Sprintf(
			" These datasets are also reachable, but only for these specific tables or views: %s.",
			strings.Join(fragments, "; "))

		if hasSingleDatasetDefault {
			datasetParam = parameters.NewStringParameter(datasetKey, datasetDescription, parameters.WithStringDefault(singleDatasetDefault))
		} else {
			datasetParam = parameters.NewStringParameter(datasetKey, datasetDescription)
		}
	}

	// The project parameter's description/default must reflect the union of
	// every project referenced by allowedDatasets and allowedTables, not just
	// allowedDatasets. A single allowedDatasets entry does NOT mean a single
	// project is allowed when allowedTables references a different project;
	// claiming otherwise would make that table effectively unreachable via
	// the tool's own documented interface.
	projectSet := make(map[string]struct{})
	for _, ds := range allowedDatasets {
		parts := strings.Split(ds, ".")
		if len(parts) >= 1 && parts[0] != "" {
			projectSet[parts[0]] = struct{}{}
		}
	}
	for _, t := range allowedTables {
		parts := strings.Split(t, ".")
		if len(parts) == 3 {
			projectSet[parts[0]] = struct{}{}
		}
	}

	switch len(projectSet) {
	case 0:
		// No dataset/table allowlist to derive a project from; leave
		// projectDescription/defaultProjectID untouched.
	case 1:
		var project string
		for p := range projectSet {
			project = p
		}
		defaultProjectID = project
		projectDescription += fmt.Sprintf(" Must be `%s`.", project)
	default:
		projects := make([]string, 0, len(projectSet))
		for p := range projectSet {
			projects = append(projects, fmt.Sprintf("`%s`", p))
		}
		sort.Strings(projects)
		projectDescription += fmt.Sprintf(" Must be one of the following: %s.", strings.Join(projects, ", "))
		// Multiple projects are allowed; do not lock the default to a single
		// one of them, preserve whatever default the caller passed in.
	}

	projectParam = parameters.NewStringParameter(projectKey, projectDescription, parameters.WithStringDefault(defaultProjectID))

	return projectParam, datasetParam
}

// StripSingleQuotes removes leading and trailing single quotes from a string if both are present.
func StripSingleQuotes(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}

// ValidColumnParam returns true if s (stripped of leading/trailing single quotes) is a safe column name.
func ValidColumnParam(s string) bool {
	return ValidColumnName(StripSingleQuotes(s))
}

// ValidContributionMetricParam returns true if s (stripped of leading/trailing single quotes) is a safe contribution metric (does not contain single quotes).
func ValidContributionMetricParam(s string) bool {
	return !strings.ContainsRune(StripSingleQuotes(s), '\'')
}
