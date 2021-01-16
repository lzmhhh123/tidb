package executor

import (
	"context"
	"encoding/json"
	"github.com/pingcap/errors"
	"github.com/pingcap/tidb/config"
	"github.com/pingcap/tidb/types"
	"github.com/pingcap/tidb/util/chunk"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

var _ Executor = &FlinkExec{}

type FlinkExec struct {
	baseExecutor

	executed bool

	sqlType string
	sql     string
	results []interface{}
	rowIdx  int
}

func (e *FlinkExec) Open(ctx context.Context) error {
	e.sqlType = strings.Split(e.sql, " ")[0]
	e.sqlType = strings.ToLower(e.sqlType)
	switch e.sqlType {
	case "select":
		e.sqlType = "query"
	default:
		e.sqlType = "ddlOrInsert"
	}
	return nil
}

func (e *FlinkExec) Next(ctx context.Context, req *chunk.Chunk) error {
	if !e.executed {
		resp, err := http.PostForm(config.GetGlobalConfig().FlinkAddr + "/proxy-server/" + e.sqlType, url.Values{"sql": {e.sql}})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		var res map[string]interface{}
		err = json.Unmarshal(body, &res)
		if err != nil {
			return err
		}
		if res["errorMsg"].(string) != "" {
			return errors.New(res["errorMsg"].(string))
		}
		if e.sqlType == "query" {
			e.results = res["result"].([]interface{})
		}
		e.executed = true
	}
	req.Reset()
	for !req.IsFull() {
		if e.rowIdx >= len(e.results) {
			break
		}
		for i, d := range e.results[e.rowIdx].([]interface{}) {
			if d == nil {
				req.AppendNull(i)
			} else {
				strVal := types.NewStringDatum(d.(string))
				val, err := strVal.ConvertTo(e.ctx.GetSessionVars().StmtCtx, e.Schema().Columns[i].RetType)
				if err != nil {
					return err
				}
				req.AppendDatum(i, &val)
			}
		}
		e.rowIdx++
	}
	return nil
}

func (e *FlinkExec) Close() error {
	return nil
}
