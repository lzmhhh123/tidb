// Copyright 2019 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.
package executor

import (
	"log"
	"strings"
	"testing"

	"github.com/pingcap/parser"
)

func TestAddHint(t *testing.T) {
	sql := "select * from t limit 100000, 100"
	text := "explain analyze" + sql
	normalizeExplainSQL := parser.Normalize(text)
	log.Println(normalizeExplainSQL)
	idx := strings.Index(normalizeExplainSQL, "select")
	normalizeSQL := normalizeExplainSQL[idx:]
	log.Println(normalizeSQL)
	hash := parser.DigestHash(normalizeSQL)
	log.Println(normalizeSQL, hash)

	text = sql

	normalizeSQL, hash = parser.NormalizeDigest(text)
	log.Println(normalizeSQL, hash)

	nsql := "select a from t limit 100,10"
	nsql = parser.Normalize(nsql)
	log.Println(nsql)
	nsql = parser.Normalize(nsql)
	log.Println(nsql)
}
