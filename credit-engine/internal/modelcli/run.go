// Package modelcli implements the modelgen command without exiting the process.
package modelcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

type modelPaths []string

func (p *modelPaths) String() string { return fmt.Sprint([]string(*p)) }
func (p *modelPaths) Set(value string) error {
	if value == "" {
		return fmt.Errorf("model path is empty")
	}
	*p = append(*p, value)
	return nil
}

func Run(args []string, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprintln(stderr, "Usage: modelgen validate|generate|check|archive [-root credit-engine] [-release VERSION] [-model models/NAME.json ...] [-notes TEXT]")
		fmt.Fprintln(stderr, "generate writes fixed model artifacts; check and validate do not write; archive writes models/releases/VERSION.zip and its immutable manifest.")
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	command := args[0]
	if command == "help" || command == "-h" || command == "--help" {
		usage()
		return 0
	}
	if command != "validate" && command != "generate" && command != "check" && command != "archive" {
		usage()
		return 2
	}
	flags := flag.NewFlagSet("modelgen "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "credit-engine module root")
	version := flags.String("release", "", "immutable release version; defaults to the current manifest")
	notes := flags.String("notes", "", "release description")
	var inputs modelPaths
	flags.Var(&inputs, "model", "relative model definition path; repeat to select the complete catalog")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(flags.Args()) > 0 {
		usage()
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(stderr, err); return 1 }
	write := func(value any) int {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(value); err != nil {
			return fail(err)
		}
		return 0
	}
	workspace, err := release.OpenWorkspace(*root)
	if err != nil {
		return fail(err)
	}
	defer workspace.Close()
	if len(inputs) == 0 {
		paths, err := workspace.ModelPaths()
		if err != nil {
			return fail(err)
		}
		inputs = modelPaths(paths)
	}
	definitions := []model.ModelDefinition{}
	for _, name := range inputs {
		data, err := workspace.Read(name)
		if err != nil {
			return fail(err)
		}
		definition, err := model.Decode(bytes.NewReader(data))
		if err != nil {
			return fail(fmt.Errorf("%s: %w", name, err))
		}
		definitions = append(definitions, definition)
	}
	if command == "validate" {
		if _, err := modelgen.GenerateModels(definitions); err != nil {
			return fail(err)
		}
		versions := []string{}
		for _, definition := range definitions {
			if !release.ValidVersion(definition.Version) || strings.EqualFold(definition.Version, "release") {
				return fail(fmt.Errorf("model version cannot be emitted as a portable source path: %s", definition.Version))
			}
			versions = append(versions, definition.Version)
		}
		return write(struct {
			Valid  bool     `json:"valid"`
			Models []string `json:"models"`
		}{true, versions})
	}
	if *version == "" {
		current, err := workspace.Current()
		if err != nil {
			return fail(err)
		}
		if current == nil {
			fmt.Fprintln(stderr, "-release is required before the first release")
			return 2
		}
		*version = current.Version
	}
	base, existing, err := workspace.Base(*version)
	if err != nil {
		return fail(err)
	}
	notesSet := false
	flags.Visit(func(f *flag.Flag) { notesSet = notesSet || f.Name == "notes" })
	if existing != nil && !notesSet {
		*notes = existing.Notes
	}
	markdown, err := workspace.Read("docs/02_inputs_and_scoring.md")
	if err != nil {
		return fail(err)
	}
	bundle, err := release.Prepare(definitions, release.Options{Version: *version, Markdown: markdown, Notes: *notes, Previous: base})
	if err != nil {
		return fail(err)
	}
	switch command {
	case "check":
		differences, err := workspace.Check(bundle)
		if err != nil {
			return fail(err)
		}
		rc := write(struct {
			Current     bool                 `json:"current"`
			Release     string               `json:"release"`
			SHA256      string               `json:"manifest_sha256"`
			Differences []release.Difference `json:"differences"`
		}{len(differences) == 0, *version, bundle.SHA256(), differences})
		if rc == 0 && len(differences) > 0 {
			return 1
		}
		return rc
	case "generate":
		if err := workspace.Apply(bundle); err != nil {
			return fail(err)
		}
		return write(struct {
			Release string `json:"release"`
			SHA256  string `json:"manifest_sha256"`
			Files   int    `json:"files"`
		}{*version, bundle.SHA256(), len(bundle.Artifacts())})
	case "archive":
		name, checksum, err := workspace.WriteArchive(bundle)
		if err != nil {
			return fail(err)
		}
		return write(struct {
			Release        string `json:"release"`
			Path           string `json:"path"`
			SHA256         string `json:"archive_sha256"`
			ManifestSHA256 string `json:"manifest_sha256"`
		}{*version, name, checksum, bundle.SHA256()})
	}
	return 2
}
