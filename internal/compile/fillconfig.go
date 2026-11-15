package compile

import (
	"strconv"
	"time"

	"github.com/vinodhalaharvi/tilegen/internal/sexp"
)

// FillConfig is how `tilegen fill` talks to an LLM on this machine.
type FillConfig struct {
	Command string
	Retries int
	Timeout time.Duration
	Build   string
}

func DefaultFill() FillConfig {
	return FillConfig{Retries: 1, Timeout: 5 * time.Minute, Build: "go build ./..."}
}

func parseFill(n *sexp.Node, fc *FillConfig, bad func(*sexp.Node, string, ...any)) {
	for _, it := range n.Args() {
		if len(it.List) != 2 || it.List[1].IsList {
			bad(it, "expected (%s value), got %s", it.Head(), Short(it))
			continue
		}
		val := it.List[1].Atom
		switch it.Head() {
		case "command":
			fc.Command = val
		case "build":
			fc.Build = val
		case "retries":
			r, err := strconv.Atoi(val)
			if err != nil || r < 0 || r > 5 {
				bad(it, "retries must be 0 to 5, got %s", val)
			}
			fc.Retries = r
		case "timeout":
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				bad(it, "timeout must be a duration like \"5m\", got %s", val)
			}
			fc.Timeout = d
		default:
			bad(it, "fill takes (command ...), (retries N), (timeout \"5m\") and (build ...), got %s%s",
				Short(it), DidYouMean(it.Head(), []string{"command", "retries", "timeout", "build"}))
		}
	}
}
