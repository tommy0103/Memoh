package compaction

import (
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	historyfrag "github.com/felinics/memoh/internal/agent/context/history"
	userinput "github.com/felinics/memoh/internal/agent/decision/input"
)

type CompactPolicy string

const (
	CompactPolicyPreserveRecent      CompactPolicy = "preserve_recent"
	CompactPolicyPreserveToolClosure CompactPolicy = "preserve_tool_closure"
	CompactPolicyMustKeep            CompactPolicy = "must_keep"
)

// CompactionCandidate is the typed view of one uncompacted history row used
// during candidate selection. RawContent and RawUsage retain the raw row payload
// so token estimation stays byte-identical to the legacy path; Record carries the
// typed classifier output used for policies, tool-aware boundaries, and
// summarizer rendering.
type CompactionCandidate struct {
	ID           pgtype.UUID
	RawContent   []byte
	RawUsage     []byte
	Record       historyfrag.HistoryRecord
	Policies     []CompactPolicy
	IsToolResult bool
}

func (c CompactionCandidate) HasPolicy(policy CompactPolicy) bool {
	for _, p := range c.Policies {
		if p == policy {
			return true
		}
	}
	return false
}

// toolExchangeGroups partitions items into tool-exchange groups: a maximal run
// starting at a non-result row that carries the tool-closure policy (an
// assistant tool-call row) followed by every immediately-adjacent tool-result
// row answering it. Rows outside any exchange are returned as singleton
// groups, so an exchange can never be split by ID-blind index slicing.
func toolExchangeGroups(items []CompactionCandidate) [][]int {
	groups := make([][]int, 0, len(items))
	for i := 0; i < len(items); {
		if isToolResultItem(items[i]) || !items[i].HasPolicy(CompactPolicyPreserveToolClosure) {
			groups = append(groups, []int{i})
			i++
			continue
		}
		group := []int{i}
		j := i + 1
		for j < len(items) && isToolResultItem(items[j]) {
			group = append(group, j)
			j++
		}
		groups = append(groups, group)
		i = j
	}
	return groups
}

// protectPendingUserInput keeps an ask_user exchange in raw history while any
// of its calls has no answer: the continuation still needs the open call. An
// answered exchange is ordinary history the summary can cover.
func protectPendingUserInput(items []CompactionCandidate) {
	for _, group := range toolExchangeGroups(items) {
		if isToolResultItem(items[group[0]]) {
			continue
		}
		pending := askUserCallIDs(items[group[0]].Record)
		for _, idx := range group[1:] {
			mm := items[idx].Record.ModelMessage
			delete(pending, strings.TrimSpace(mm.ToolCallID))
			for _, p := range parseEntryParts(mm.Content) {
				delete(pending, strings.TrimSpace(p.ToolCallID))
			}
		}
		if len(pending) == 0 {
			continue
		}
		for _, idx := range group {
			items[idx].Policies = appendPolicy(items[idx].Policies, CompactPolicyMustKeep)
		}
	}
}

func askUserCallIDs(record historyfrag.HistoryRecord) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, call := range record.ModelMessage.ToolCalls {
		if isAskUserToolName(call.Function.Name) {
			ids[strings.TrimSpace(call.ID)] = struct{}{}
		}
	}
	for _, p := range parseEntryParts(record.ModelMessage.Content) {
		if isToolCallPartType(p.Type) && isAskUserToolName(p.ToolName) {
			ids[strings.TrimSpace(p.ToolCallID)] = struct{}{}
		}
	}
	return ids
}

// propagateMustKeepAcrossToolExchanges spreads CompactPolicyMustKeep across an
// entire tool exchange: if the call row or any of its answering tool-result
// rows is must-keep, every row in that exchange becomes must-keep, so
// excluding only the must-keep row can never orphan a sibling tool call/result.
func propagateMustKeepAcrossToolExchanges(items []CompactionCandidate) {
	for _, group := range toolExchangeGroups(items) {
		if len(group) < 2 {
			continue
		}
		mustKeep := false
		for _, idx := range group {
			if items[idx].HasPolicy(CompactPolicyMustKeep) {
				mustKeep = true
				break
			}
		}
		if !mustKeep {
			continue
		}
		for _, idx := range group {
			items[idx].Policies = appendPolicy(items[idx].Policies, CompactPolicyMustKeep)
		}
	}
}

func markSelectionPolicies(items []CompactionCandidate) {
	if latestUser := latestUserIndex(items); latestUser == 0 && len(items) > 1 {
		tailStart := recentTailProtectedStart(items, 1)
		for i := range items {
			if i == 0 || i >= tailStart {
				items[i].Policies = appendPolicy(items[i].Policies, CompactPolicyPreserveRecent)
			}
		}
		return
	}

	start := recentProtectedStart(items)
	for i := start; i < len(items); i++ {
		items[i].Policies = appendPolicy(items[i].Policies, CompactPolicyPreserveRecent)
	}
}

func recentProtectedStart(items []CompactionCandidate) int {
	if len(items) == 0 {
		return 0
	}
	if latestUser := latestUserIndex(items); latestUser >= 0 {
		return latestUser
	}
	return recentTailProtectedStart(items, 0)
}

func latestUserIndex(items []CompactionCandidate) int {
	for i := len(items) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(items[i].Record.ModelMessage.Role), "user") {
			return i
		}
	}
	return -1
}

func recentTailProtectedStart(items []CompactionCandidate, minStart int) int {
	start := len(items) - 1
	for start > minStart && isToolClosureResultItem(items[start]) {
		start--
	}
	return start
}

func candidatePolicies(record historyfrag.HistoryRecord) []CompactPolicy {
	var policies []CompactPolicy
	if isToolExchangeRecord(record) {
		policies = appendPolicy(policies, CompactPolicyPreserveToolClosure)
	}
	if isAskUserRecord(record) {
		policies = appendPolicy(policies, CompactPolicyPreserveToolClosure)
	}
	return policies
}

func appendPolicy(policies []CompactPolicy, policy CompactPolicy) []CompactPolicy {
	for _, p := range policies {
		if p == policy {
			return policies
		}
	}
	return append(policies, policy)
}

func isToolExchangeRecord(record historyfrag.HistoryRecord) bool {
	mm := record.ModelMessage
	if strings.EqualFold(strings.TrimSpace(mm.Role), "tool") {
		return true
	}
	if len(mm.ToolCalls) > 0 {
		return true
	}
	for _, p := range parseEntryParts(mm.Content) {
		if isToolPartType(p.Type) {
			return true
		}
	}
	return false
}

func isAskUserRecord(record historyfrag.HistoryRecord) bool {
	mm := record.ModelMessage
	if isAskUserToolName(mm.Name) {
		return true
	}
	for _, call := range mm.ToolCalls {
		if isAskUserToolName(call.Function.Name) {
			return true
		}
	}
	for _, p := range parseEntryParts(mm.Content) {
		if isAskUserToolName(p.ToolName) {
			return true
		}
	}
	return false
}

func isAskUserToolName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), userinput.ToolNameAskUser)
}

type usagePayload struct {
	OutputTokens      *int `json:"outputTokens"`
	OutputTokensSnake *int `json:"output_tokens"`
}

func estimateItemTokens(item CompactionCandidate) int {
	if len(item.RawUsage) > 0 {
		var u usagePayload
		if json.Unmarshal(item.RawUsage, &u) == nil {
			if u.OutputTokens != nil && *u.OutputTokens > 0 {
				return *u.OutputTokens
			}
			if u.OutputTokensSnake != nil && *u.OutputTokensSnake > 0 {
				return *u.OutputTokensSnake
			}
		}
	}
	return len(item.RawContent) / 4
}

func estimateBytesAsTokens(value string) int {
	if value == "" {
		return 0
	}
	return (len(value) + 3) / 4
}

// splitByRatio splits items so that roughly the first ratio% (by token weight)
// are returned for compaction, and the rest are kept as-is.
func splitByRatio(items []CompactionCandidate, totalInputTokens, ratio int) []CompactionCandidate {
	if ratio <= 0 || totalInputTokens <= 0 || len(items) == 0 {
		return nil
	}
	if ratio >= 100 {
		return guardedCompactionItems(items, len(items))
	}

	keepTokens := totalInputTokens * (100 - ratio) / 100
	if keepTokens <= 0 {
		return guardedCompactionItems(items, len(items))
	}

	accumulated := 0
	cutoff := len(items)
	for i := len(items) - 1; i >= 0; i-- {
		accumulated += estimateItemTokens(items[i])
		if accumulated >= keepTokens {
			cutoff = i + 1
			break
		}
	}

	if cutoff <= 0 {
		return nil
	}
	return guardedCompactionItems(items, cutoff)
}

// splitByTarget returns the oldest items to compact so that the remaining newest
// items fit within targetTokens. Used by synchronous compaction.
func splitByTarget(items []CompactionCandidate, targetTokens int) []CompactionCandidate {
	if targetTokens <= 0 || len(items) == 0 {
		return nil
	}
	accumulated := 0
	cutoff := 0
	for i := len(items) - 1; i >= 0; i-- {
		accumulated += estimateItemTokens(items[i])
		if accumulated > targetTokens {
			cutoff = i + 1
			break
		}
	}
	if cutoff <= 0 {
		return nil
	}
	return guardedCompactionItems(items, cutoff)
}

func guardedCompactionItems(items []CompactionCandidate, cutoff int) []CompactionCandidate {
	if cutoff <= 0 || len(items) == 0 {
		return nil
	}
	protectedStart := firstPolicyStart(items, CompactPolicyPreserveRecent)
	if protectedStart <= 0 {
		return guardedCurrentTurnCompactionItems(items, cutoff)
	}
	if cutoff > protectedStart {
		cutoff = protectedStart
	}
	cutoff = adjustForToolBoundary(items, cutoff)
	if cutoff > protectedStart {
		return nil
	}
	if cutoff <= 0 {
		return nil
	}
	return items[:cutoff]
}

func guardedCurrentTurnCompactionItems(items []CompactionCandidate, cutoff int) []CompactionCandidate {
	if len(items) <= 1 {
		return nil
	}
	protectedTailStart := firstPolicyStartAfter(items, CompactPolicyPreserveRecent, 1)
	if protectedTailStart <= 1 {
		return nil
	}
	if cutoff > protectedTailStart {
		cutoff = protectedTailStart
	}
	if cutoff <= 1 {
		return nil
	}
	cutoff = adjustForToolBoundary(items, cutoff)
	if cutoff > protectedTailStart {
		return nil
	}
	return items[1:cutoff]
}

func firstPolicyStart(items []CompactionCandidate, policy CompactPolicy) int {
	return firstPolicyStartAfter(items, policy, 0)
}

func firstPolicyStartAfter(items []CompactionCandidate, policy CompactPolicy, start int) int {
	if start < 0 {
		start = 0
	}
	for i, item := range items {
		if i < start {
			continue
		}
		if item.HasPolicy(policy) {
			return i
		}
	}
	return len(items)
}

// adjustForToolBoundary moves the compact/keep cutoff forward so the kept
// (newest) side never begins with an orphan tool result whose tool call is
// being compacted. Tool results are pulled into the compact set so each tool
// exchange stays intact on one side of the boundary.
func adjustForToolBoundary(items []CompactionCandidate, cutoff int) int {
	for cutoff > 0 && cutoff < len(items) && isToolClosureResultItem(items[cutoff]) {
		cutoff++
	}
	return cutoff
}

func isToolClosureResultItem(item CompactionCandidate) bool {
	return item.HasPolicy(CompactPolicyPreserveToolClosure) && isToolResultItem(item)
}

func isToolResultItem(item CompactionCandidate) bool {
	return item.IsToolResult
}

// buildEntriesAndIDs renders the summarizer entries and the ids to mark
// compacted from one contiguous run of tool-exchange groups (see
// toolExchangeGroups). Must-keep groups (a pending ask_user call, unparseable
// barriers) stay in raw history and split the span into runs; the first run
// that renders any entry wins and is covered whole. A row that renders
// nothing (a reasoning-only message, an empty tool result) is covered with its
// neighbours and contributes no entry: leaving it raw would split the run into
// fragments too short to summarize. One run under one compact_id keeps the
// marked ids a contiguous history range, which the read path
// (replaceCompactedHistoryRecords) replaces in place.
func buildEntriesAndIDs(items []CompactionCandidate) ([]messageEntry, []pgtype.UUID) {
	groups := toolExchangeGroups(items)
	mustKeep := func(group []int) bool {
		for _, idx := range group {
			if items[idx].HasPolicy(CompactPolicyMustKeep) {
				return true
			}
		}
		return false
	}
	for g := 0; g < len(groups); {
		if mustKeep(groups[g]) {
			g++
			continue
		}
		runEnd := g
		for runEnd < len(groups) && !mustKeep(groups[runEnd]) {
			runEnd++
		}
		var entries []messageEntry
		var ids []pgtype.UUID
		for _, group := range groups[g:runEnd] {
			for _, idx := range group {
				ids = append(ids, items[idx].ID)
				if content := renderCandidateEntry(items[idx].Record); strings.TrimSpace(content) != "" {
					entries = append(entries, messageEntry{Role: items[idx].Record.ModelMessage.Role, Content: content})
				}
			}
		}
		if len(entries) > 0 {
			return entries, ids
		}
		g = runEnd
	}
	return nil, nil
}

// trimCompactMessages caps one compaction call's input to maxTokens by keeping
// the oldest tool-exchange groups and deferring the newest overflow to a later
// pass. Chewing front-to-back reclaims the oldest raw rows first and keeps
// summary coverage in chronological order across passes, so prior summaries
// read in narrative order.
//
// The budget models exactly what buildEntriesAndIDs will feed the summarizer:
// must-keep groups and rows that render nothing cost nothing. The first
// group with a cost is always kept so an oversized one cannot stall progress,
// and group-aligned cuts can never split a tool exchange.
func trimCompactMessages(items []CompactionCandidate, maxTokens int) []CompactionCandidate {
	if len(items) == 0 || maxTokens <= 0 {
		return items
	}
	groups := toolExchangeGroups(items)
	total := 0
	for _, group := range groups {
		total += markableGroupCost(items, group)
	}
	if total <= maxTokens {
		return items
	}
	accumulated := 0
	end := 0
	keptMarkableGroup := false
	for _, group := range groups {
		cost := markableGroupCost(items, group)
		if cost > 0 && keptMarkableGroup && accumulated+cost > maxTokens {
			break
		}
		accumulated += cost
		end = group[len(group)-1] + 1
		if cost > 0 {
			keptMarkableGroup = true
		}
	}
	return items[:end]
}

// markableGroupCost is the summarizer-prompt cost of one tool-exchange group:
// zero for a must-keep group, otherwise each rendered entry's text — what
// buildUserPrompt actually emits, headers included — plus its role prefix,
// with a floor of one token per entry so tiny-but-real entries can never ride
// along for free. Rows that render nothing cost nothing. Costing the rendered
// bytes rather than the raw usage estimate matters: a row whose generation
// cost five tokens can still render a two-kilobyte tool outcome into the
// prompt.
func markableGroupCost(items []CompactionCandidate, group []int) int {
	cost := 0
	for _, idx := range group {
		if items[idx].HasPolicy(CompactPolicyMustKeep) {
			return 0
		}
		rendered := strings.TrimSpace(renderCandidateEntry(items[idx].Record))
		if rendered == "" {
			continue
		}
		cost += estimateBytesAsTokens(rendered) + estimateBytesAsTokens(items[idx].Record.ModelMessage.Role) + 1
	}
	return cost
}

// markableCompactCost sums markableGroupCost across the span — the tokens its
// entries will actually occupy in the summarizer prompt.
func markableCompactCost(items []CompactionCandidate) int {
	total := 0
	for _, group := range toolExchangeGroups(items) {
		total += markableGroupCost(items, group)
	}
	return total
}
