package compile

import "flag"

// ParseAnywhere parses flags that may come before, between or after the
// positional arguments (Go's flag package stops at the first non-flag, so
// `tilegen check spec/ -allow-holes` would otherwise ignore the flag).
func ParseAnywhere(fs *flag.FlagSet, args []string) []string {
	var pos []string
	for {
		fs.Parse(args)
		if fs.NArg() == 0 {
			return pos
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
