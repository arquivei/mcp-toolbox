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

package internal

import (
	"context"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/googleapis/mcp-toolbox/internal/prebuiltconfigs"
	"github.com/googleapis/mcp-toolbox/internal/sources"
)

// Patch Qive: o driver Oracle (godror) exige CGO, e o build da imagem é
// CGO_ENABLED=0. Este teste quebra se um rebase devolver o import do Oracle
// para imports.go. Ver QIVE-FORK.md.
func TestImports_OracleSourceIsNotRegistered(t *testing.T) {
	ctx := context.Background()
	decode := func(sourceType string) error {
		_, err := sources.DecodeConfig(ctx, sourceType, "x", yaml.NewDecoder(strings.NewReader("")))
		return err
	}

	if err := decode("oracle"); err == nil || !strings.Contains(err.Error(), `unknown source type: "oracle"`) {
		t.Fatalf(`source "oracle" deveria estar fora do build; err = %v`, err)
	}
	if err := decode("bigquery"); err != nil && strings.Contains(err.Error(), "unknown source type") {
		t.Fatalf(`source "bigquery" precisa continuar registrado; err = %v`, err)
	}
}

// Sem o source Oracle, a config pré-pronta "oracledb" falharia no startup;
// ela sai do binário junto com o driver.
func TestImports_OraclePrebuiltConfigIsNotEmbedded(t *testing.T) {
	if _, err := prebuiltconfigs.Get("oracledb"); err == nil {
		t.Fatal(`prebuilt "oracledb" não deveria existir sem o source Oracle`)
	}
}
