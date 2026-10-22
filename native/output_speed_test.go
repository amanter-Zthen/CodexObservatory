package main

import (
	"codex-observatory/internal/routetrace"
	"strings"
	"testing"
)

func TestOutputSpeedLabelsKeepStreamingCharactersSeparateFromReportedTokens(t *testing.T) {
	progress := routetrace.Record{Stage: "response.output_text.delta", CharactersPerSecond: 12.5, TokensPerSecond: 100}
	if label := outputSpeedLabel(progress); !strings.Contains(label, "12.5 字/秒") || strings.Contains(label, "token") {
		t.Fatal(label)
	}
	complete := routetrace.Record{Stage: "response.completed", OutputTokens: 120, TokensPerSecond: 60}
	if label := outputSpeedLabel(complete); !strings.Contains(label, "60.0 token/秒") || !strings.Contains(label, "本次响应") {
		t.Fatal(label)
	}
	complete.OutputTokens = 0
	if outputSpeedLabel(complete) != "" {
		t.Fatal("missing usage became a token measurement")
	}
}
