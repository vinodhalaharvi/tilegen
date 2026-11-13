//go:build !js

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

// tilegen key set | remove | status
//
// The one secret tilegen ever holds is an Anthropic API key, for the
// optional step that asks Claude to draft a tile. It is kept in the
// operating system's keychain (macOS Keychain, Windows Credential
// Manager, the Secret Service on Linux), never in a file tilegen writes
// and never on a command line, where it would stay in shell history.
//
// ANTHROPIC_API_KEY in the environment is used first when it is set, so a
// machine with no keychain, or a CI job, needs nothing stored.

const keyService, keyUser = "tilegen", "anthropic-api-key"

// anthropicKey returns the key and where it came from, or "" when there
// is none.
func anthropicKey() (key, from string) {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return k, "the ANTHROPIC_API_KEY environment variable"
	}
	if k, err := keyring.Get(keyService, keyUser); err == nil && k != "" {
		return k, "the keychain"
	}
	return "", ""
}

func keyCmd(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: tilegen key set | remove | status")
	}
	switch args[0] {
	case "set":
		key, err := readSecret(in, out, "Anthropic API key (not shown): ")
		if err != nil {
			return err
		}
		if !strings.HasPrefix(key, "sk-ant-") {
			return errors.New("that does not look like an Anthropic API key: they start with sk-ant-. Nothing was stored")
		}
		if err := keyring.Set(keyService, keyUser, key); err != nil {
			return fmt.Errorf("the keychain would not take it (%v). On a machine without one, set ANTHROPIC_API_KEY in the environment instead", err)
		}
		fmt.Fprintln(out, "Stored in the keychain. tilegen serve will use it; `tilegen key remove` deletes it.")
		return nil
	case "remove":
		err := keyring.Delete(keyService, keyUser)
		if errors.Is(err, keyring.ErrNotFound) {
			fmt.Fprintln(out, "There was no key in the keychain.")
			return nil
		}
		if err != nil {
			return fmt.Errorf("the keychain would not remove it: %v", err)
		}
		fmt.Fprintln(out, "Removed from the keychain.")
		return nil
	case "status":
		if key, from := anthropicKey(); key != "" {
			fmt.Fprintf(out, "A key is set, from %s (ends …%s).\n", from, key[len(key)-4:])
		} else {
			fmt.Fprintln(out, "No key is set. Asking Claude to draft tiles is off; everything else works without one.")
		}
		return nil
	}
	return fmt.Errorf("unknown: tilegen key %s (want set, remove or status)", args[0])
}

// readSecret reads one line without echoing it when the input is a
// terminal, and plainly when it is piped.
func readSecret(in io.Reader, out io.Writer, prompt string) (string, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(out, prompt)
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(out)
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no key was given: type it at the prompt, or pipe it in")
	}
	return strings.TrimSpace(line), nil
}
