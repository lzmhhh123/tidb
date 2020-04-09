// Copyright 2018 PingCAP, Inc.
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

package planner

import (
	"context"
	"strings"

	"github.com/pingcap/errors"
	"github.com/pingcap/parser"
	"github.com/pingcap/parser/ast"
	"github.com/pingcap/parser/model"
	"github.com/pingcap/parser/mysql"
	"github.com/pingcap/parser/opcode"
	"github.com/pingcap/tidb/bindinfo"
	"github.com/pingcap/tidb/domain"
	"github.com/pingcap/tidb/expression"
	"github.com/pingcap/tidb/infoschema"
	"github.com/pingcap/tidb/kv"
	"github.com/pingcap/tidb/metrics"
	"github.com/pingcap/tidb/planner/cascades"
	plannercore "github.com/pingcap/tidb/planner/core"
	"github.com/pingcap/tidb/privilege"
	"github.com/pingcap/tidb/sessionctx"
	"github.com/pingcap/tidb/sessionctx/stmtctx"
	"github.com/pingcap/tidb/statistics"
	"github.com/pingcap/tidb/types"
	driver "github.com/pingcap/tidb/types/parser_driver"
	"github.com/pingcap/tidb/util/hint"
	"github.com/pingcap/tidb/util/logutil"
	"github.com/pingcap/tidb/util/ranger"
	"go.uber.org/zap"
)

// Optimize does optimization and creates a Plan.
// The node must be prepared first.
func Optimize(ctx context.Context, sctx sessionctx.Context, node ast.Node, is infoschema.InfoSchema) (plannercore.Plan, types.NameSlice, error) {
	if _, isolationReadContainTiKV := sctx.GetSessionVars().GetIsolationReadEngines()[kv.TiKV]; isolationReadContainTiKV {
		fp := plannercore.TryFastPlan(sctx, node)
		if fp != nil {
			if !useMaxTS(sctx, fp) {
				sctx.PrepareTSFuture(ctx)
			}
			return fp, fp.OutputNames(), nil
		}
	}

	sctx.PrepareTSFuture(ctx)

	tableHints := hint.ExtractTableHintsFromStmtNode(node)
	stmtHints, warns := handleStmtHints(tableHints)
	defer func() {
		sctx.GetSessionVars().StmtCtx.StmtHints = stmtHints
		for _, warn := range warns {
			sctx.GetSessionVars().StmtCtx.AppendWarning(warn)
		}
	}()
	sctx.GetSessionVars().StmtCtx.StmtHints = stmtHints
	bestPlan, names, _, err := optimize(ctx, sctx, node, is)
	if err != nil {
		return nil, nil, err
	}
	if !(sctx.GetSessionVars().UsePlanBaselines || sctx.GetSessionVars().EvolvePlanBaselines) {
		return bestPlan, names, nil
	}
	stmtNode, ok := node.(ast.StmtNode)
	if !ok {
		return bestPlan, names, nil
	}
	bindRecord, scope, selectivity := getBindRecordAndSelectivity(sctx, stmtNode)
	if bindRecord == nil {
		return bestPlan, names, nil
	}
	bucketID := int64(selectivity * float64(sctx.GetSessionVars().SPMSpaceNumber))
	if bucketID >= sctx.GetSessionVars().SPMSpaceNumber {
		bucketID = sctx.GetSessionVars().SPMSpaceNumber - 1
	}

	bestPlanHint := plannercore.GenHintsFromPhysicalPlan(bestPlan)
	if bindRecord.NormalizedBinding != nil {
		orgBinding := bindRecord.NormalizedBinding // the first is the original binding
		for _, tbHint := range tableHints {        // consider table hints which contained by the original binding
			if orgBinding.Hint.ContainTableHint(tbHint.HintName.String()) {
				bestPlanHint = append(bestPlanHint, tbHint)
			}
		}
	}
	bestPlanHintStr := hint.RestoreOptimizerHints(bestPlanHint)

	// If the best bestPlan is in baselines, just use it.
	baseline := bindRecord.FindBaseline(bucketID)
	if baseline != nil && baseline.Status == bindinfo.Using &&
		baseline.ID == bestPlanHintStr+baseline.BucketIdSuffix() {
		if sctx.GetSessionVars().UsePlanBaselines {
			stmtHints, warns = handleStmtHints(baseline.Hint.GetFirstTableHints())
		}
		return bestPlan, names, nil
	}

	var bestPlanAmongHints plannercore.Plan
	originHints := hint.CollectHint(stmtNode)
	// Try to find the best binding.
	useBaseline := false
	if baseline != nil && baseline.Status == bindinfo.Using && !baseline.Fixed {
		metrics.BindUsageCounter.WithLabelValues(scope).Inc()
		hint.BindHint(stmtNode, baseline.Hint)
		curStmtHints, curWarns := handleStmtHints(baseline.Hint.GetFirstTableHints())
		sctx.GetSessionVars().StmtCtx.StmtHints = curStmtHints
		plan, _, _, err := optimize(ctx, sctx, node, is)
		if err != nil {
			baseline.Status = bindinfo.Invalid
			handleInvalidBindRecord(ctx, sctx, scope, bindinfo.BindRecord{
				OriginalSQL: bindRecord.OriginalSQL,
				Db:          bindRecord.Db,
				Baselines:   map[int64]*bindinfo.Binding{bucketID: baseline},
			})
		} else {
			if sctx.GetSessionVars().UsePlanBaselines {
				stmtHints, warns = curStmtHints, curWarns
			}
			useBaseline = true
			bestPlanAmongHints = plan
		}
	}
	// If none baseline aims at selectivity bucket or baseline is invalid. We use the normalized binding instead.
	if !useBaseline {
		binding := bindRecord.NormalizedBinding
		if binding != nil && binding.Status == bindinfo.Using && !binding.Fixed {
			metrics.BindUsageCounter.WithLabelValues(scope).Inc()
			hint.BindHint(stmtNode, binding.Hint)
			curStmtHints, curWarns := handleStmtHints(binding.Hint.GetFirstTableHints())
			sctx.GetSessionVars().StmtCtx.StmtHints = curStmtHints
			plan, _, _, err := optimize(ctx, sctx, node, is)
			if err != nil {
				binding.Status = bindinfo.Invalid
				handleInvalidBindRecord(ctx, sctx, scope, bindinfo.BindRecord{
					OriginalSQL:       bindRecord.OriginalSQL,
					Db:                bindRecord.Db,
					NormalizedBinding: binding,
				})
			} else {
				if sctx.GetSessionVars().UsePlanBaselines {
					stmtHints, warns = curStmtHints, curWarns
				}
				bestPlanAmongHints = plan
			}
		}
	}
	// 1. If there is already a evolution task, we do not need to handle it again.
	// 2. If the origin binding contain `read_from_storage` hint, we should ignore the evolve task.
	// 3. If the best plan contain TiFlash hint, we should ignore the evolve task.
	if sctx.GetSessionVars().EvolvePlanBaselines &&
		!originHints.ContainTableHint(plannercore.HintReadFromStorage) &&
		!bindRecord.GetFirstBinding().Hint.ContainTableHint(plannercore.HintReadFromStorage) {
		err := handleEvolveTasks(ctx, sctx, bindRecord, stmtNode, bestPlanHintStr, bucketID)
		if err != nil {
			logutil.Logger(ctx).Warn("add baseline evolution task error", zap.Error(err))
		}
	}
	// Restore the hint to avoid changing the stmt node.
	hint.BindHint(stmtNode, originHints)
	if sctx.GetSessionVars().UsePlanBaselines && bestPlanAmongHints != nil {
		return bestPlanAmongHints, names, nil
	}
	return bestPlan, names, nil
}

func optimize(ctx context.Context, sctx sessionctx.Context, node ast.Node, is infoschema.InfoSchema) (plannercore.Plan, types.NameSlice, float64, error) {
	// build logical plan
	sctx.GetSessionVars().PlanID = 0
	sctx.GetSessionVars().PlanColumnID = 0
	hintProcessor := &hint.BlockHintProcessor{Ctx: sctx}
	node.Accept(hintProcessor)
	builder := plannercore.NewPlanBuilder(sctx, is, hintProcessor)
	p, err := builder.Build(ctx, node)
	if err != nil {
		return nil, nil, 0, err
	}

	sctx.GetSessionVars().StmtCtx.Tables = builder.GetDBTableInfo()
	activeRoles := sctx.GetSessionVars().ActiveRoles
	// Check privilege. Maybe it's better to move this to the Preprocess, but
	// we need the table information to check privilege, which is collected
	// into the visitInfo in the logical plan builder.
	if pm := privilege.GetPrivilegeManager(sctx); pm != nil {
		if err := plannercore.CheckPrivilege(activeRoles, pm, builder.GetVisitInfo()); err != nil {
			return nil, nil, 0, err
		}
	}

	if err := plannercore.CheckTableLock(sctx, is, builder.GetVisitInfo()); err != nil {
		return nil, nil, 0, err
	}

	// Handle the execute statement.
	if execPlan, ok := p.(*plannercore.Execute); ok {
		err := execPlan.OptimizePreparedPlan(ctx, sctx, is)
		return p, p.OutputNames(), 0, err
	}

	names := p.OutputNames()

	// Handle the non-logical plan statement.
	logic, isLogicalPlan := p.(plannercore.LogicalPlan)
	if !isLogicalPlan {
		return p, names, 0, nil
	}

	// Handle the logical plan statement, use cascades planner if enabled.
	if sctx.GetSessionVars().GetEnableCascadesPlanner() {
		finalPlan, cost, err := cascades.DefaultOptimizer.FindBestPlan(sctx, logic)
		return finalPlan, names, cost, err
	}
	finalPlan, cost, err := plannercore.DoOptimize(ctx, sctx, builder.GetOptFlag(), logic)
	return finalPlan, names, cost, err
}

func extractSelectAndNormalizeDigest(stmtNode ast.StmtNode) (*ast.SelectStmt, string, string) {
	switch x := stmtNode.(type) {
	case *ast.ExplainStmt:
		switch x.Stmt.(type) {
		case *ast.SelectStmt:
			plannercore.EraseLastSemicolon(x)
			normalizeExplainSQL := parser.Normalize(x.Text())
			idx := strings.Index(normalizeExplainSQL, "select")
			normalizeSQL := normalizeExplainSQL[idx:]
			hash := parser.DigestNormalized(normalizeSQL)
			return x.Stmt.(*ast.SelectStmt), normalizeSQL, hash
		}
	case *ast.SelectStmt:
		plannercore.EraseLastSemicolon(x)
		normalizedSQL, hash := parser.NormalizeDigest(x.Text())
		return x, normalizedSQL, hash
	}
	return nil, "", ""
}

func extractSelectivity(ctx sessionctx.Context, sel *ast.SelectStmt) float64 {
	if sel.From == nil || sel.From.TableRefs == nil {
		return 0
	}
	// TODO: consider multiple join and subquery
	var tables []*model.TableInfo
	if leftTbl, ok := sel.From.TableRefs.Left.(*ast.TableSource); ok {
		if tblName, ok := leftTbl.Source.(*ast.TableName); ok && tblName.TableInfo != nil {
			tables = append(tables, tblName.TableInfo)
		}
	}
	if sel.From.TableRefs.Right != nil {
		if rightTbl, ok := sel.From.TableRefs.Right.(*ast.TableSource); ok {
			if tblName, ok := rightTbl.Source.(*ast.TableName); ok && tblName.TableInfo != nil {
				tables = append(tables, tblName.TableInfo)
			}
		}
	}
	// TODO: consider the subquery where conditions
	conditions := plannercore.SplitWhere(sel.Where)
	if len(conditions) == 0 {
		return 0
	}
	if len(tables) == 0 {
		return 0
	}
	for _, cond := range conditions {
		if _, ok := cond.(*ast.BinaryOperationExpr); !ok {
			continue
		}
		compareExpr := cond.(*ast.BinaryOperationExpr)
		if compareExpr.Op != opcode.GE && compareExpr.Op != opcode.LE && compareExpr.Op != opcode.EQ &&
			compareExpr.Op != opcode.GT && compareExpr.Op != opcode.LT {
			continue
		}
		var constant *expression.Constant
		var columnName *ast.ColumnName
		switch v := compareExpr.L.(type) {
		case *driver.ValueExpr:
			constant = &expression.Constant{Value: v.Datum, RetType: &v.Type}
		case *ast.ColumnNameExpr:
			columnName = v.Name
		}
		switch v := compareExpr.R.(type) {
		case *driver.ValueExpr:
			constant = &expression.Constant{Value: v.Datum, RetType: &v.Type}
		case *ast.ColumnNameExpr:
			columnName = v.Name
		}
		if constant == nil || columnName == nil {
			continue
		}
		for _, tblInfo := range tables {
			var statisticsColumn *statistics.Column
			var col *model.ColumnInfo
			var statsTbl *statistics.Table
			for _, colInfo := range tblInfo.Columns {
				if colInfo.Name.L == columnName.Name.L {
					statsTbl = plannercore.GetStatsTable(ctx, tblInfo, tblInfo.ID)
					statisticsColumn = statsTbl.ColumnByName(colInfo.Name.L)
					col = colInfo
					break
				}
			}
			if statisticsColumn == nil || col == nil || statsTbl == nil {
				continue
			}
			ranExpr := expression.NewFunctionInternal(ctx, compareExpr.Op.String(), types.NewFieldType(mysql.TypeTiny), &expression.Column{
				RetType:  &col.FieldType,
				ID:       col.ID,
				UniqueID: ctx.GetSessionVars().AllocPlanColumnID(),
				Index:    col.Offset,
				OrigName: col.Name.L,
				IsHidden: col.Hidden,
			}, constant)

			// We ignore error here, because there may be some truncate here.
			ran, err := ranger.BuildColumnRange([]expression.Expression{ranExpr}, ctx.GetSessionVars().StmtCtx, statisticsColumn.Tp, types.UnspecifiedLength)
			if err != nil {
				// do nothing, to avoid error check
			}
			if len(ran) == 0 {
				return -1
			}
			evaluatedCount, err := statisticsColumn.GetColumnRowCount(ctx.GetSessionVars().StmtCtx, ran, statsTbl.ModifyCount, tblInfo.PKIsHandle)
			if err != nil {
				return -1
			}
			return evaluatedCount / float64(statsTbl.Count)
		}
	}
	return 0
}

func getBindRecordAndSelectivity(ctx sessionctx.Context, stmt ast.StmtNode) (*bindinfo.BindRecord, string, float64) {
	// When the domain is initializing, the bind will be nil.
	if ctx.Value(bindinfo.SessionBindInfoKeyType) == nil {
		return nil, "", -1
	}
	selectStmt, normalizedSQL, hash := extractSelectAndNormalizeDigest(stmt)
	if selectStmt == nil {
		return nil, "", -1
	}
	sessionHandle := ctx.Value(bindinfo.SessionBindInfoKeyType).(*bindinfo.SessionHandle)
	bindRecord := sessionHandle.GetBindRecord(normalizedSQL, ctx.GetSessionVars().CurrentDB)
	if bindRecord == nil {
		bindRecord = sessionHandle.GetBindRecord(normalizedSQL, "")
	}
	if bindRecord != nil {
		if bindRecord.HasUsingBinding() {
			return bindRecord, metrics.ScopeSession, extractSelectivity(ctx, selectStmt)
		}
		return nil, "", -1
	}
	globalHandle := domain.GetDomain(ctx).BindHandle()
	if globalHandle == nil {
		return nil, "", -1
	}
	bindRecord = globalHandle.GetBindRecord(hash, normalizedSQL, ctx.GetSessionVars().CurrentDB)
	if bindRecord == nil {
		bindRecord = globalHandle.GetBindRecord(hash, normalizedSQL, "")
	}
	return bindRecord, metrics.ScopeGlobal, extractSelectivity(ctx, selectStmt)
}

func handleInvalidBindRecord(ctx context.Context, sctx sessionctx.Context, level string, bindRecord bindinfo.BindRecord) {
	sessionHandle := sctx.Value(bindinfo.SessionBindInfoKeyType).(*bindinfo.SessionHandle)
	err := sessionHandle.DropBindRecord(bindRecord.OriginalSQL, bindRecord.Db, bindRecord.GetFirstBinding())
	if err != nil {
		logutil.Logger(ctx).Info("drop session bindings failed")
	}
	if level == metrics.ScopeSession {
		return
	}

	globalHandle := domain.GetDomain(sctx).BindHandle()
	globalHandle.AddDropInvalidBindTask(&bindRecord)
}

func handleEvolveTasks(ctx context.Context, sctx sessionctx.Context, br *bindinfo.BindRecord, stmtNode ast.StmtNode, planHint string, bucketID int64) error {
	bindSQL := bindinfo.GenerateBindSQL(ctx, stmtNode, planHint)
	if bindSQL == "" {
		return nil
	}
	charset, collation := sctx.GetSessionVars().GetCharsetInfo()
	binding := bindinfo.Binding{
		BindSQL:   bindSQL,
		Status:    bindinfo.PendingVerify,
		Charset:   charset,
		Collation: collation,
		BindType:  bindinfo.Baseline,
		BucketID:  bucketID,
	}
	globalHandle := domain.GetDomain(sctx).BindHandle()
	return globalHandle.AddEvolvePlanTask(br.OriginalSQL, br.Db, binding)
}

// useMaxTS returns true when meets following conditions:
//  1. ctx is auto commit tagged.
//  2. plan is point get by pk.
func useMaxTS(ctx sessionctx.Context, p plannercore.Plan) bool {
	if !plannercore.IsAutoCommitTxn(ctx) {
		return false
	}

	v, ok := p.(*plannercore.PointGetPlan)
	return ok && v.IndexInfo == nil
}

// OptimizeExecStmt to optimize prepare statement protocol "execute" statement
// this is a short path ONLY does things filling prepare related params
// for point select like plan which does not need extra things
func OptimizeExecStmt(ctx context.Context, sctx sessionctx.Context,
	execAst *ast.ExecuteStmt, is infoschema.InfoSchema) (plannercore.Plan, error) {
	var err error
	builder := plannercore.NewPlanBuilder(sctx, is, nil)
	p, err := builder.Build(ctx, execAst)
	if err != nil {
		return nil, err
	}
	if execPlan, ok := p.(*plannercore.Execute); ok {
		err = execPlan.OptimizePreparedPlan(ctx, sctx, is)
		return execPlan.Plan, err
	}
	err = errors.Errorf("invalid result plan type, should be Execute")
	return nil, err
}

func handleStmtHints(hints []*ast.TableOptimizerHint) (stmtHints stmtctx.StmtHints, warns []error) {
	if len(hints) == 0 {
		return
	}
	var memoryQuotaHint, useToJAHint, useCascadesHint, maxExecutionTime *ast.TableOptimizerHint
	var memoryQuotaHintCnt, useToJAHintCnt, useCascadesHintCnt, noIndexMergeHintCnt, readReplicaHintCnt, maxExecutionTimeCnt int
	for _, hint := range hints {
		switch hint.HintName.L {
		case "memory_quota":
			memoryQuotaHint = hint
			memoryQuotaHintCnt++
		case "use_toja":
			useToJAHint = hint
			useToJAHintCnt++
		case "use_cascades":
			useCascadesHint = hint
			useCascadesHintCnt++
		case "no_index_merge":
			noIndexMergeHintCnt++
		case "read_consistent_replica":
			readReplicaHintCnt++
		case "max_execution_time":
			maxExecutionTimeCnt++
			maxExecutionTime = hint
		}
	}
	// Handle MEMORY_QUOTA
	if memoryQuotaHintCnt != 0 {
		if memoryQuotaHintCnt > 1 {
			warn := errors.New("There are multiple MEMORY_QUOTA hints, only the last one will take effect")
			warns = append(warns, warn)
		}
		// Executor use MemoryQuota <= 0 to indicate no memory limit, here use < 0 to handle hint syntax error.
		if memoryQuota := memoryQuotaHint.HintData.(int64); memoryQuota < 0 {
			warn := errors.New("The use of MEMORY_QUOTA hint is invalid, valid usage: MEMORY_QUOTA(10 MB) or MEMORY_QUOTA(10 GB)")
			warns = append(warns, warn)
		} else {
			stmtHints.HasMemQuotaHint = true
			stmtHints.MemQuotaQuery = memoryQuota
			if memoryQuota == 0 {
				warn := errors.New("Setting the MEMORY_QUOTA to 0 means no memory limit")
				warns = append(warns, warn)
			}
		}
	}
	// Handle USE_TOJA
	if useToJAHintCnt != 0 {
		if useToJAHintCnt > 1 {
			warn := errors.New("There are multiple USE_TOJA hints, only the last one will take effect")
			warns = append(warns, warn)
		}
		stmtHints.HasAllowInSubqToJoinAndAggHint = true
		stmtHints.AllowInSubqToJoinAndAgg = useToJAHint.HintData.(bool)
	}
	// Handle USE_CASCADES
	if useCascadesHintCnt != 0 {
		if useCascadesHintCnt > 1 {
			warn := errors.Errorf("USE_CASCADES() is defined more than once, only the last definition takes effect: USE_CASCADES(%v)", useCascadesHint.HintData.(bool))
			warns = append(warns, warn)
		}
		stmtHints.HasEnableCascadesPlannerHint = true
		stmtHints.EnableCascadesPlanner = useCascadesHint.HintData.(bool)
	}
	// Handle NO_INDEX_MERGE
	if noIndexMergeHintCnt != 0 {
		if noIndexMergeHintCnt > 1 {
			warn := errors.New("There are multiple NO_INDEX_MERGE hints, only the last one will take effect")
			warns = append(warns, warn)
		}
		stmtHints.NoIndexMergeHint = true
	}
	// Handle READ_CONSISTENT_REPLICA
	if readReplicaHintCnt != 0 {
		if readReplicaHintCnt > 1 {
			warn := errors.New("There are multiple READ_CONSISTENT_REPLICA hints, only the last one will take effect")
			warns = append(warns, warn)
		}
		stmtHints.HasReplicaReadHint = true
		stmtHints.ReplicaRead = byte(kv.ReplicaReadFollower)
	}
	// Handle MAX_EXECUTION_TIME
	if maxExecutionTimeCnt != 0 {
		if maxExecutionTimeCnt > 1 {
			warn := errors.New("There are multiple MAX_EXECUTION_TIME hints, only the last one will take effect")
			warns = append(warns, warn)
		}
		stmtHints.HasMaxExecutionTime = true
		stmtHints.MaxExecutionTime = maxExecutionTime.HintData.(uint64)
	}
	return
}

func init() {
	plannercore.OptimizeAstNode = Optimize
}
