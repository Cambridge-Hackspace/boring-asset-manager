package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// ImportOptions are bam import's choices (#145, README §2.3).
type ImportOptions struct {
	// Anchor is the category, by name or ID, the archive's top categories
	// go under; empty for the top level.
	Anchor string
	// Choose are what to do with what clashes, each kind:name=choice.
	Choose []string
	// Only are the kinds of a backup to merge; nil for all that can be.
	Only []archive.Kind
	// Check says what importing would do, changing nothing.
	Check bool
}

// chooseKinds are what --choose names, as the plan's kinds.
var chooseKinds = map[string]string{
	"category": archive.ItemCategory, "field": archive.ItemField, "hook": archive.ItemHook, "link-type": archive.ItemRelationType,
	"label-template": archive.ItemLabelTemplate, "scan-template": archive.ItemScanTemplate, "report": archive.ItemReport,
	"flow": archive.ItemFlow, "scope": archive.ItemScope, "image": archive.ItemImage, "icon-file": archive.ItemIconFile,
	"category-icon": archive.ItemCategoryIcon,
}

// Import merges a package, or the kinds chosen of a backup, into the
// instance, after saying what it will do; with Check, it only says.
func Import(configPath, from string, opts ImportOptions) error {
	if from == "" {
		return errors.New("--from is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()
	in := archive.ImportOptions{Kinds: opts.Only, Icons: isolex.Default()}
	if opts.Anchor != "" {
		if in.Anchor, err = idNamed(st, "categories", "name", archive.ItemCategory, opts.Anchor); err != nil {
			return fmt.Errorf("--anchor: %w", err)
		}
	}
	open := func() (io.ReadCloser, error) { return os.Open(from) }
	ctx := context.Background()
	if len(opts.Choose) > 0 {
		// the names chosen by are the plan's
		plan, err := archive.CheckImport(ctx, st, open, in)
		if plan == nil {
			return err
		}
		if in.Choices, err = ParseChoices(plan, opts.Choose); err != nil {
			return err
		}
	}
	plan, err := archive.CheckImport(ctx, st, open, in)
	if plan != nil {
		printImportPlan(os.Stdout, plan)
	}
	if err != nil {
		return err
	}
	if opts.Check {
		return nil
	}
	if len(plan.Problems) > 0 {
		return errors.New("not imported, for the problems above")
	}
	blobs, err := filestore.Open(cfg.Storage)
	if err != nil {
		return err
	}
	in.Fingerprint = plan.Fingerprint
	done, err := archive.Import(ctx, st, blobs, open, in)
	if err != nil {
		return err
	}
	if err := keepOwners(st, cfg); err != nil {
		return fmt.Errorf("imported, but giving the files it made to their directories' owners: %w", err)
	}
	if _, err := st.RebuildIcons(ctx, in.Icons); err != nil && !errors.Is(err, isolex.ErrUnavailable) {
		fmt.Printf("Note: the icons will be built when BAM next starts: %v.\n", err)
	}
	made := 0
	for _, it := range done.Items {
		if it.Action == archive.ChoiceCreate || it.Action == archive.ChoiceCopy {
			made++
		}
	}
	assets := 0
	if done.Assets != nil {
		assets = done.Assets.Assets
	}
	fmt.Printf("Imported %d things, %d assets and %d files (%d bytes) from %s.\n", made, assets, done.Files, done.FileBytes, from)
	return nil
}

// ParseChoices reads --choose's items, each kind:name=choice, naming what
// the plan holds by its name, key or path, or its ID in the archive.
func ParseChoices(plan *archive.ImportPlan, items []string) (map[string]archive.Choice, error) {
	out := map[string]archive.Choice{}
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			what, choice, ok := strings.Cut(part, "=")
			kind, name, ok2 := strings.Cut(what, ":")
			k, known := chooseKinds[kind]
			c := archive.Choice(choice)
			if !ok || !ok2 || !known || (c != archive.ChoiceExisting && c != archive.ChoiceCopy && c != archive.ChoiceSkip) {
				return nil, fmt.Errorf("--choose %q: kind:name=existing, copy or skip", part)
			}
			var found []archive.ImportItem
			for _, it := range plan.Items {
				if it.Kind == k && (it.ID == name || it.Name == name || strings.HasSuffix(it.Name, " ("+name+")")) {
					found = append(found, it)
				}
			}
			switch len(found) {
			case 0:
				return nil, fmt.Errorf("--choose %q: the archive holds no %s %q", part, kind, name)
			case 1:
				out[found[0].Key()] = c
			default:
				return nil, fmt.Errorf("--choose %q: %q names %d of them; give one's ID", part, name, len(found))
			}
		}
	}
	return out, nil
}

// printImportPlan says what importing does with each thing, and why not
// when it cannot.
func printImportPlan(w io.Writer, plan *archive.ImportPlan) {
	what := "backup"
	if p := plan.Header.Package; p != nil {
		what = fmt.Sprintf("package %s", p.Name)
		if p.Version != "" {
			what += " " + p.Version
		}
	}
	under := "at the top level"
	if plan.Anchor != nil {
		under = "under " + plan.Anchor.Name
	}
	fmt.Fprintf(w, "Importing the %s, its categories %s:\n", what, under)
	for _, it := range plan.Items {
		line := fmt.Sprintf("  %-8s %s %s", it.Action, strings.ReplaceAll(it.Kind, "_", " "), it.Name)
		if it.As != "" {
			line += " as " + it.As
		}
		if it.Existing != nil {
			same := "differs from"
			if it.Identical {
				same = "is the same as"
			}
			var choices []string
			for _, c := range it.Choices {
				choices = append(choices, string(c))
			}
			line += fmt.Sprintf(" (%s %s here", same, it.Existing.Name)
			if len(choices) > 0 {
				line += "; or " + strings.Join(choices, ", ")
			}
			line += ")"
		}
		if it.Note != "" {
			line += ". " + it.Note
		}
		fmt.Fprintln(w, line)
	}
	if a := plan.Assets; a != nil {
		fmt.Fprintf(w, "  %d assets, %d photos, %d links and %d events of their history\n", a.Assets, a.Photos, a.Links, a.Events)
		if a.ValuesDropped > 0 || a.LinksDropped > 0 {
			fmt.Fprintf(w, "  without %d values of fields skipped and %d links of link types skipped\n", a.ValuesDropped, a.LinksDropped)
		}
		if a.CodesTaken > 0 {
			fmt.Fprintf(w, "  %d codes are another asset's here, and are left out\n", a.CodesTaken)
		}
	}
	fmt.Fprintf(w, "  %d files (%d bytes)\n", plan.Files, plan.FileBytes)
	if len(plan.Permissions) > 0 {
		fmt.Fprintf(w, "It needs %s.\n", strings.Join(plan.Permissions, "; "))
	}
	for _, m := range plan.Warnings {
		fmt.Fprintln(w, "Note: "+m)
	}
	for _, p := range plan.Problems {
		fmt.Fprintln(w, "Problem: "+p)
	}
}
