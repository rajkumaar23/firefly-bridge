package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rajkumaar23/firefly-bridge/internal/datadir"
	"github.com/sirupsen/logrus"
)

// ensureBrowserProfile makes sure a browser profile exists in the central
// data directory before the first run of a new binary. It returns the
// legacy directory the profile was copied from ("" when no legacy profile
// was found or a fresh one was started), so callers can migrate sibling
// files such as .state.json from the same place.
//
// The old binary stored the profile CWD-relative, but release binaries are
// run from anywhere — usually the home directory — so a CWD-relative
// "first run" migration silently found nothing and the user lost every
// logged-in session. Instead of guessing, ask: if the user says they have
// used firefly-bridge before, they point at the project directory that
// holds the old profile (it is usually the cloned repo directory), which is
// then copied into the data directory (original left in place). Otherwise a
// fresh profile is started.
//
// It is headless-safe: a closed stdin (cron, pipes) answers "no" and gets a
// fresh profile instead of blocking.
func ensureBrowserProfile(dataDir string, logger *logrus.Logger, stdin io.Reader) (string, error) {
	profileDir := filepath.Join(dataDir, "chromedp-data")
	// A profile with sessions already lives here (or the browser just
	// created an empty one) — nothing to do, never prompt.
	if st, err := os.Stat(profileDir); err == nil && st.IsDir() {
		return "", nil
	}

	// One scanner for the whole session: a fresh scanner per line would
	// buffer the following answers ahead and silently drop them.
	in := bufio.NewScanner(stdin)
	fmt.Print("No browser profile yet in the data dir.\n")
	ok, err := askYesNo(in)
	if err != nil || !ok {
		// No answer, or "no": start fresh.
		if err != nil {
			logger.Infof("no interactive input available — starting a fresh browser profile in %s", profileDir)
		} else {
			logger.Infof("starting a fresh browser profile in %s", profileDir)
		}
		return "", os.MkdirAll(profileDir, 0o700)
	}

	src, err := askLegacyProfileDir(in)
	if err != nil {
		logger.Warnf("could not use the provided profile location (%v) — starting a fresh browser profile in %s instead; you may need to log in again", err, profileDir)
		return "", os.MkdirAll(profileDir, 0o700)
	}
	if err := datadir.CopyDir(src, profileDir); err != nil {
		return "", fmt.Errorf("copying browser profile from %s: %w", src, err)
	}
	logger.Infof("migrated browser profile %s → %s (original left in place)", src, profileDir)
	return src, nil
}

// askYesNo prompts "yes" (y/yes, case-insensitive; empty counts as no).
func askYesNo(in *bufio.Scanner) (bool, error) {
	fmt.Print("Has firefly-bridge been used before, with logged-in sessions in an older install? [y/N] ")
	answer, err := readLine(in)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// askLegacyProfileDir asks where the old CWD-relative browser profile
// lives. The old binary kept it in the working directory (typically the
// cloned repo directory), so the user can answer with either that project
// directory or the chromedp-data/ path itself. It validates that the answer
// actually looks like a profile and re-asks otherwise; an empty answer
// cancels.
func askLegacyProfileDir(in *bufio.Scanner) (string, error) {
	for {
		fmt.Print("Project directory containing the old chromedp-data/ profile (or the chromedp-data path itself; enter to skip): ")
		answer, err := readLine(in)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		// An empty answer (or a clean EOF, which readLine reports as "")
		// cancels the migration. Check before Abs: filepath.Abs("") returns
		// the CWD, so it would otherwise look like a real (bad) path and
		// the prompt would loop forever.
		if answer == "" {
			return "", fmt.Errorf("no directory provided")
		}
		expanded, err := filepath.Abs(answer)
		if err != nil {
			return "", err
		}
		// Accept both "<project>/chromedp-data" and "<project>" itself.
		if datadir.IsProfile(expanded) {
			return expanded, nil
		}
		inside := filepath.Join(expanded, "chromedp-data")
		if datadir.IsProfile(inside) {
			return inside, nil
		}
		fmt.Printf("%s does not look like a chromedp profile (no Default/ inside, and no chromedp-data/ inside it) — try another.\n", expanded)
	}
}

// readLine reads one line of input; EOF ends the prompt.
func readLine(in *bufio.Scanner) (string, error) {
	if !in.Scan() {
		return "", in.Err() // nil on clean EOF
	}
	return in.Text(), nil
}
