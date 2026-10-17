// Command licenses copies notices for modules linked into the exporter.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Dir string
	Main               bool
}
type dependency struct{ Module *module }

func main() {
	output := flag.String("output", filepath.Join(".tmp", "THIRD_PARTY_NOTICES"), "Generated notices file")
	flag.Parse()
	if err := generate(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(destination string) error {
	cmd := exec.Command("go", "list", "-deps", "-json", "./cmd/napcat_exporter")
	// Enumerate the shipped Linux binary even when generating notices on Windows.
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return err
	}
	modules := map[string]module{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var dep dependency
		err := decoder.Decode(&dep)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if dep.Module != nil && !dep.Module.Main {
			modules[dep.Module.Path] = *dep.Module
		}
	}
	goRoot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	modules["Go"] = module{Path: "Go", Dir: strings.TrimSpace(string(goRoot))}
	var keys []string
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var notices strings.Builder
	notices.WriteString("Third-party notices\n\nLicenses and notices for the exporter's linked modules and Go toolchain.\n")
	for _, key := range keys {
		mod := modules[key]
		fmt.Fprintf(&notices, "\n============================================================\n%s %s\n============================================================\n", mod.Path, mod.Version)
		entries, err := os.ReadDir(mod.Dir)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			upper := strings.ToUpper(entry.Name())
			if entry.IsDir() {
				continue
			}
			isNotice := strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") || upper == "NOTICE" || upper == "COPYRIGHT" || upper == "PATENTS"
			if !isNotice {
				continue
			}
			found = true
			content, err := os.ReadFile(filepath.Join(mod.Dir, entry.Name()))
			if err != nil {
				return err
			}
			fmt.Fprintf(&notices, "\n--- %s ---\n\n", entry.Name())
			notices.Write(content)
			notices.WriteString("\n")
		}
		if !found {
			return fmt.Errorf("no license found for %s", mod.Path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	return os.WriteFile(destination, []byte(notices.String()), 0644)
}
