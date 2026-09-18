package aec

import (
	"fmt"
	"io"
)

// loadForCLI returns the effective rules and the overlay for a CLI
// command. Unlike the hooks it creates the starter overlay when none
// exists and prints warnings on stderr. A broken overlay is an error,
// not a fallback to the defaults.
func loadForCLI(stderr io.Writer) (Merged, *overlayFile, error) {
	path, err := OverlayPath()
	if err != nil {
		return Merged{}, nil, err
	}
	wrote, err := ensureOverlay(path)
	if err != nil {
		return Merged{}, nil, err
	}
	if wrote {
		fmt.Fprintf(stderr, "aec: wrote starter overlay to %s\n", path)
	}
	ov, err := LoadOverlay(path)
	if err != nil {
		return Merged{}, nil, validationErr("%v", err)
	}
	defaults, err := LoadDefaults()
	if err != nil {
		return Merged{}, nil, err
	}
	merged, err := Merge(defaults, ov)
	if err != nil {
		return Merged{}, nil, validationErr("%s: %v", path, err)
	}
	for _, name := range merged.Stale {
		fmt.Fprintf(stderr, "aec: overlay: %q is not a default rule and is not a complete rule; ignored\n", name)
	}
	if ov != nil {
		for _, key := range ov.Undecoded {
			fmt.Fprintf(stderr, "aec: overlay: unknown key %q; ignored\n", key)
		}
	}
	return merged, ov, nil
}
