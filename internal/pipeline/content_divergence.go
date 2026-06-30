package pipeline

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// contentDivergenceThreshold is the minimum character delta before a stream vs
// result mismatch is treated as significant. Smaller gaps are normal when the
// SDK consolidates text between tool_use/tool_result differently than streaming
// deltas.
const contentDivergenceThreshold = 500

// contentDivergence describes how accumulated streaming text differs from the
// authoritative bridge result content.
type contentDivergence struct {
	StreamLen int
	ResultLen int
	Diff      int
}

func detectContentDivergence(streamed, result string) (contentDivergence, bool) {
	if streamed == "" || result == "" || streamed == result {
		return contentDivergence{}, false
	}
	diff := len(streamed) - len(result)
	if diff < 0 {
		diff = -diff
	}
	return contentDivergence{
		StreamLen: len(streamed),
		ResultLen: len(result),
		Diff:      diff,
	}, true
}

func (d contentDivergence) significant() bool {
	return d.Diff > contentDivergenceThreshold
}

func (d contentDivergence) metadataJSON() string {
	b, err := json.Marshal(map[string]any{
		"stream_len":    d.StreamLen,
		"result_len":    d.ResultLen,
		"diff":          d.Diff,
		"authoritative": "result",
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}

func (d contentDivergence) message() string {
	return fmt.Sprintf("stream_len=%d result_len=%d diff=%d authoritative=result",
		d.StreamLen, d.ResultLen, d.Diff)
}

func logContentDivergence(d contentDivergence) {
	slog.Warn("bridge: result content diverges from accumulated assistant text",
		"stream_len", d.StreamLen,
		"result_len", d.ResultLen,
		"diff", d.Diff,
		"authoritative", "result")
}