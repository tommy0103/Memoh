package native

import (
	"strconv"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	tools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
)

const toolUsageGroupID = "system.tool_usage"

type toolUsageSection struct {
	// capability is the tool whose registration this guidance follows; the
	// capability gate drops the fragment when that tool is not available.
	// Empty for provider notices, which exist precisely because a tool is
	// missing and must survive the gate.
	capability string
	// id names the fragment when capability is empty.
	id   string
	text string
}

func (s toolUsageSection) fragID() string {
	if s.id != "" {
		return s.id
	}
	return s.capability
}

// toolNoticeSectionID names the usage section a provider's ToolNotice lands
// in. Notices carry no registered tool, so the provider label (or its
// position) keeps the fragment id unique instead of a tool name.
func toolNoticeSectionID(provider tools.ToolProvider, index int) string {
	label := ""
	if labeler, ok := provider.(tools.ProviderLabeler); ok {
		label = strings.TrimSpace(labeler.ProviderLabel())
	}
	if label == "" {
		label = strconv.Itoa(index)
	}
	return "notice." + label
}

func firstToolName(providerTools []toolexec.Tool) string {
	first := ""
	for _, tool := range providerTools {
		name := strings.TrimSpace(tool.Name)
		if name != "" && (first == "" || name < first) {
			first = name
		}
	}
	return first
}

func structuredToolUsage(sections []toolUsageSection, scope contextfrag.Scope) []contextfrag.ContextFrag {
	if len(sections) == 0 {
		return nil
	}
	render := contextfrag.RenderPolicy{
		Format:      contextfrag.RenderMarkdown,
		GroupID:     toolUsageGroupID,
		GroupJoiner: "\n\n",
	}
	frag := func(id, text, capability string, index int) contextfrag.ContextFrag {
		return contextfrag.TextFrag(contextfrag.TextFragInput{
			ID:                 id,
			Kind:               contextfrag.KindToolUsage,
			Role:               sdk.MessageRoleSystem,
			Slot:               contextfrag.SlotSystem,
			Text:               text,
			Priority:           45,
			RetentionTier:      contextfrag.RetentionPreferred,
			RequiredCapability: capability,
			CacheClass:         contextfrag.CacheStable,
			Trust:              contextfrag.TrustSystem,
			Scope:              scope,
			Source:             contextfrag.SourceAgentToolUsage,
			SourceID:           capability,
			Collector:          "assemble_tools",
			Index:              index,
			Render:             render,
		})
	}

	frags := make([]contextfrag.ContextFrag, 0, len(sections)+1)
	frags = append(frags, frag(
		toolUsageGroupID+".header",
		"## Tool usage",
		sections[0].capability,
		0,
	))
	for i, section := range sections {
		frags = append(frags, frag(
			toolUsageGroupID+"."+section.fragID(),
			section.text,
			section.capability,
			i+1,
		))
	}
	return frags
}
