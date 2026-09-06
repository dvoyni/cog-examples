package main

import (
	"strings"
	"testing"
)

func cc0(name string) asset {
	return asset{
		Name:    name,
		Title:   name,
		Commit:  "0123456789abcdef0123456789abcdef01234567",
		Outputs: []output{{Variant: "glTF", File: name + ".glb"}},
		Legal: []legalEntry{{
			SPDX: "CC0-1.0", What: "Everything", Artist: "Someone", Owner: "Public", Year: "2020",
			Text: "Creative Commons Zero v1.0 Universal",
			URL:  licenseURL["CC0-1.0"],
		}},
	}
}

// withBrokenSource makes a one-asset manifest that satisfies validate's rule
// that the truncated file is cut from something in the set.
func withBrokenSource(a asset) []asset {
	a.Name = brokenSource.Asset
	return []asset{a}
}

func TestTheRealManifestPasses(t *testing.T) {
	if err := validate(manifest); err != nil {
		t.Fatal(err)
	}
}

// TestValidateEnforcesTheLicencePolicy is the guard the whole vendoring story
// rests on. An NC or ND term reaching assets/ is a problem that surfaces at
// publication, long after the decision that let it in.
func TestValidateEnforcesTheLicencePolicy(t *testing.T) {
	for _, spdx := range []string{"CC-BY-NC-4.0", "CC-BY-ND-4.0", "LicenseRef-Custom", "MIT"} {
		a := cc0("Whatever")
		a.Legal[0].SPDX = spdx
		if err := validate(withBrokenSource(a)); err == nil {
			t.Errorf("%s was accepted", spdx)
		}
	}
}

func TestValidateRequiresALicenceAndNotOnlyAMark(t *testing.T) {
	a := cc0("Whatever")
	a.Legal = []legalEntry{{What: "A logo", Text: "Some Trademark or Logo"}}
	if err := validate(withBrokenSource(a)); err == nil {
		t.Fatal("an asset with only a legal mark was accepted")
	}
}

func TestValidateRequiresANamedMark(t *testing.T) {
	a := cc0("Whatever")
	a.Legal = append(a.Legal, legalEntry{What: "A logo"})
	if err := validate(withBrokenSource(a)); err == nil {
		t.Fatal("an unnamed legal mark was accepted")
	}
}

// TestValidateRequiresAPin refuses the thing that makes an asset set rot: no
// commit means "whatever main says today".
func TestValidateRequiresAPin(t *testing.T) {
	a := cc0("Whatever")
	a.Commit = ""
	if err := validate(withBrokenSource(a)); err == nil {
		t.Fatal("an unpinned asset was accepted")
	}
}

func TestValidateRejectsDuplicates(t *testing.T) {
	a := cc0(brokenSource.Asset)
	if err := validate([]asset{a, a}); err == nil {
		t.Fatal("the same asset listed twice was accepted")
	}
	twice := cc0(brokenSource.Asset)
	twice.Outputs = append(twice.Outputs, twice.Outputs[0])
	if err := validate([]asset{twice}); err == nil {
		t.Fatal("the same output written twice was accepted")
	}
}

// TestValidateKeepsTheBrokenFileAnchored catches the manifest edit that removes
// the asset the truncated file is cut from.
func TestValidateKeepsTheBrokenFileAnchored(t *testing.T) {
	if err := validate([]asset{cc0("SomethingElse")}); err == nil {
		t.Fatal("a manifest without the broken file's source was accepted")
	} else if !strings.Contains(err.Error(), brokenSource.Asset) {
		t.Errorf("error should name %s: %v", brokenSource.Asset, err)
	}
}

// TestEveryAssetSaysWhyItIsHere keeps the set from growing by accident. The
// three rejected candidates were rejected for stated reasons; an asset that
// cannot say what it covers has not been through that.
func TestEveryAssetSaysWhyItIsHere(t *testing.T) {
	for _, a := range manifest {
		if a.Why == "" {
			t.Errorf("%s: no reason recorded", a.Name)
		}
		if a.Title == "" || a.Summary == "" {
			t.Errorf("%s: no title or summary for ATTRIBUTION.md to credit", a.Name)
		}
	}
}
