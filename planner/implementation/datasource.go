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

package implementation

import (
	"github.com/pingcap/tidb/expression"
	"github.com/pingcap/tidb/kv"
	plannercore "github.com/pingcap/tidb/planner/core"
	"github.com/pingcap/tidb/planner/memo"
	"github.com/pingcap/tidb/statistics"
	"math"
)

// TableDualImpl implementation of PhysicalTableDual.
type TableDualImpl struct {
	baseImpl
}

// NewTableDualImpl creates a new table dual Implementation.
func NewTableDualImpl(dual *plannercore.PhysicalTableDual) *TableDualImpl {
	return &TableDualImpl{baseImpl{plan: dual}}
}

// CalcCost calculates the cost of the table dual Implementation.
func (impl *TableDualImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	return 0
}

// TableReaderImpl implementation of PhysicalTableReader.
type TableReaderImpl struct {
	baseImpl
	tblColHists *statistics.HistColl
}

// NewTableReaderImpl creates a new table reader Implementation.
func NewTableReaderImpl(reader *plannercore.PhysicalTableReader, hists *statistics.HistColl) *TableReaderImpl {
	base := baseImpl{plan: reader}
	impl := &TableReaderImpl{
		baseImpl:    base,
		tblColHists: hists,
	}
	return impl
}

// CalcCost calculates the cost of the table reader Implementation.
func (impl *TableReaderImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	reader := impl.plan.(*plannercore.PhysicalTableReader)
	width := impl.tblColHists.GetAvgRowSize(impl.plan.SCtx(), reader.Schema().Columns, false, false)
	sessVars := reader.SCtx().GetSessionVars()
	networkCost := outCount * sessVars.NetworkFactor * width
	// copTasks are run in parallel, to make the estimated cost closer to execution time, we amortize
	// the cost to cop iterator workers. According to `CopClient::Send`, the concurrency
	// is Min(DistSQLScanConcurrency, numRegionsInvolvedInScan), since we cannot infer
	// the number of regions involved, we simply use DistSQLScanConcurrency.
	copIterWorkers := float64(sessVars.DistSQLScanConcurrency)
	impl.cost = (networkCost + children[0].GetCost()) / copIterWorkers
	return impl.cost
}

// ScaleCostLimit implements Implementation interface.
func (impl *TableReaderImpl) ScaleCostLimit(costLimit float64) float64 {
	reader := impl.plan.(*plannercore.PhysicalTableReader)
	sessVars := reader.SCtx().GetSessionVars()
	copIterWorkers := float64(sessVars.DistSQLScanConcurrency)
	if math.MaxFloat64/copIterWorkers < costLimit {
		return math.MaxFloat64
	}
	return costLimit * copIterWorkers
}

// TableScanImpl implementation of PhysicalTableScan.
type TableScanImpl struct {
	baseImpl
	tblColHists *statistics.HistColl
	tblCols     []*expression.Column
}

// NewTableScanImpl creates a new table scan Implementation.
func NewTableScanImpl(ts *plannercore.PhysicalTableScan, cols []*expression.Column, hists *statistics.HistColl) *TableScanImpl {
	base := baseImpl{plan: ts}
	impl := &TableScanImpl{
		baseImpl:    base,
		tblColHists: hists,
		tblCols:     cols,
	}
	return impl
}

// CalcCost calculates the cost of the table scan Implementation.
func (impl *TableScanImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	ts := impl.plan.(*plannercore.PhysicalTableScan)
	width := impl.tblColHists.GetTableAvgRowSize(impl.plan.SCtx(), impl.tblCols, kv.TiKV, true)
	sessVars := ts.SCtx().GetSessionVars()
	impl.cost = outCount * sessVars.ScanFactor * width
	if ts.Desc {
		impl.cost = outCount * sessVars.DescScanFactor * width
	}
	return impl.cost
}

// IndexReaderImpl is the implementation of PhysicalIndexReader.
type IndexReaderImpl struct {
	baseImpl
	tblColHists *statistics.HistColl
}

// ScaleCostLimit implements Implementation interface.
func (impl *IndexReaderImpl) ScaleCostLimit(costLimit float64) float64 {
	reader := impl.plan.(*plannercore.PhysicalIndexReader)
	sessVars := reader.SCtx().GetSessionVars()
	copIterWorkers := float64(sessVars.DistSQLScanConcurrency)
	if math.MaxFloat64/copIterWorkers < costLimit {
		return math.MaxFloat64
	}
	return costLimit * copIterWorkers
}

// CalcCost implements Implementation interface.
func (impl *IndexReaderImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	reader := impl.plan.(*plannercore.PhysicalIndexReader)
	sessVars := reader.SCtx().GetSessionVars()
	networkCost := outCount * sessVars.NetworkFactor * impl.tblColHists.GetAvgRowSize(reader.SCtx(), children[0].GetPlan().Schema().Columns, true, false)
	copIterWorkers := float64(sessVars.DistSQLScanConcurrency)
	impl.cost = (networkCost + children[0].GetCost()) / copIterWorkers
	return impl.cost
}

// NewIndexReaderImpl creates a new IndexReader Implementation.
func NewIndexReaderImpl(reader *plannercore.PhysicalIndexReader, tblColHists *statistics.HistColl) *IndexReaderImpl {
	return &IndexReaderImpl{
		baseImpl:    baseImpl{plan: reader},
		tblColHists: tblColHists,
	}
}

// IndexScanImpl is the Implementation of PhysicalIndexScan.
type IndexScanImpl struct {
	baseImpl
	tblColHists *statistics.HistColl
}

// CalcCost implements Implementation interface.
func (impl *IndexScanImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	is := impl.plan.(*plannercore.PhysicalIndexScan)
	sessVars := is.SCtx().GetSessionVars()
	rowSize := impl.tblColHists.GetIndexAvgRowSize(is.SCtx(), is.Schema().Columns, is.Index.Unique)
	cost := outCount * rowSize * sessVars.ScanFactor
	if is.Desc {
		cost = outCount * rowSize * sessVars.DescScanFactor
	}
	cost += float64(len(is.Ranges)) * sessVars.SeekFactor
	impl.cost = cost
	return impl.cost
}

// NewIndexScanImpl creates a new IndexScan Implementation.
func NewIndexScanImpl(scan *plannercore.PhysicalIndexScan, tblColHists *statistics.HistColl) *IndexScanImpl {
	return &IndexScanImpl{
		baseImpl:    baseImpl{plan: scan},
		tblColHists: tblColHists,
	}
}

// IndexLookUpReaderImpl is the Implementation of PhysicalIndexLookUpReader.
type IndexLookUpReaderImpl struct {
	baseImpl

	KeepOrder   bool
	tblColHists *statistics.HistColl
	extraProj   *plannercore.PhysicalProjection
}

// NewIndexLookUpReaderImpl creates a new table reader Implementation.
func NewIndexLookUpReaderImpl(reader plannercore.PhysicalPlan, hists *statistics.HistColl, proj *plannercore.PhysicalProjection) *IndexLookUpReaderImpl {
	base := baseImpl{plan: reader}
	impl := &IndexLookUpReaderImpl{
		baseImpl:    base,
		tblColHists: hists,
		extraProj:   proj,
	}
	return impl
}

// CalcCost calculates the cost of the table reader Implementation.
func (impl *IndexLookUpReaderImpl) CalcCost(outCount float64, children ...memo.Implementation) float64 {
	var reader *plannercore.PhysicalIndexLookUpReader
	if impl.extraProj != nil {
		impl.cost += impl.extraProj.GetCost(impl.extraProj.Stats().RowCount)
	}
	reader = impl.plan.(*plannercore.PhysicalIndexLookUpReader)
	reader.IndexPlan, reader.TablePlan = children[0].GetPlan(), children[1].GetPlan()
	// Add cost of building table reader executors. Handles are extracted in batch style,
	// each handle is a range, the CPU cost of building copTasks should be:
	// (indexRows / batchSize) * batchSize * CPUFactor
	// Since we don't know the number of copTasks built, ignore these network cost now.
	sessVars := reader.SCtx().GetSessionVars()
	// Add children cost.
	impl.cost += (children[0].GetCost() + children[1].GetCost()) / float64(sessVars.DistSQLScanConcurrency)
	indexRows := reader.IndexPlan.Stats().RowCount
	impl.cost += indexRows * sessVars.CPUFactor
	// Add cost of worker goroutines in index lookup.
	numTblWorkers := float64(sessVars.IndexLookupConcurrency)
	impl.cost += (numTblWorkers + 1) * sessVars.ConcurrencyFactor
	// When building table reader executor for each batch, we would sort the handles. CPU
	// cost of sort is:
	// CPUFactor * batchSize * Log2(batchSize) * (indexRows / batchSize)
	indexLookupSize := float64(sessVars.IndexLookupSize)
	batchSize := math.Min(indexLookupSize, indexRows)
	if batchSize > 2 {
		sortCPUCost := (indexRows * math.Log2(batchSize) * sessVars.CPUFactor) / numTblWorkers
		impl.cost += sortCPUCost
	}
	// Also, we need to sort the retrieved rows if index lookup reader is expected to return
	// ordered results. Note that row count of these two sorts can be different, if there are
	// operators above table scan.
	tableRows := reader.TablePlan.Stats().RowCount
	selectivity := tableRows / indexRows
	batchSize = math.Min(indexLookupSize*selectivity, tableRows)
	if impl.KeepOrder && batchSize > 2 {
		sortCPUCost := (tableRows * math.Log2(batchSize) * sessVars.CPUFactor) / numTblWorkers
		impl.cost += sortCPUCost
	}
	return impl.cost
}

// ScaleCostLimit implements Implementation interface.
func (impl *IndexLookUpReaderImpl) ScaleCostLimit(costLimit float64) float64 {
	sessVars := impl.plan.SCtx().GetSessionVars()
	copIterWorkers := float64(sessVars.DistSQLScanConcurrency)
	if math.MaxFloat64/copIterWorkers < costLimit {
		return math.MaxFloat64
	}
	return costLimit * copIterWorkers
}

// AttachChildren implements Implementation AttachChildren interface.
func (impl *IndexLookUpReaderImpl) AttachChildren(children ...memo.Implementation) memo.Implementation {
	reader := impl.plan.(*plannercore.PhysicalIndexLookUpReader)
	reader.TablePlans = plannercore.FlattenPushDownPlan(reader.TablePlan)
	reader.IndexPlans = plannercore.FlattenPushDownPlan(reader.IndexPlan)
	return impl
}

// GetPlan implements Implementation GetPlan interface.
func (impl *IndexLookUpReaderImpl) GetPlan() plannercore.PhysicalPlan {
	if impl.extraProj != nil {
		return impl.extraProj
	}
	return impl.plan
}
