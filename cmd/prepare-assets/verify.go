package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// upstreamMetadata is the part of a model's metadata.json this tool reads.
type upstreamMetadata struct {
	Name  string `json:"name"`
	Legal []struct {
		SPDX   string `json:"spdx"`
		What   string `json:"what"`
		Artist string `json:"artist"`
		Owner  string `json:"owner"`
		Year   string `json:"year"`
		Text   string `json:"text"`
	} `json:"legal"`
}

// verifyLegal checks what the manifest says about an asset's licensing against
// what upstream says at the pinned commit.
//
// This is the check that makes the recorded terms worth anything. Reading the
// licence out of the asset at vendoring time and writing it straight into
// ATTRIBUTION.md would produce a file that is always self-consistent and never
// tells anyone anything: a relicensing upstream would flow through it silently.
// Recording the terms by hand and diffing them here means the tool stops, and a
// person looks, exactly when the terms move.
//
// It is deliberately whole-record and ordered. A changed artist or a new legal
// mark is as much a reason to look as a changed SPDX identifier.
func verifyLegal(a asset, metadata []byte) error {
	var upstream upstreamMetadata
	if err := json.Unmarshal(metadata, &upstream); err != nil {
		return fmt.Errorf("%s: read upstream metadata.json: %w", a.Name, err)
	}

	if len(upstream.Legal) != len(a.Legal) {
		return fmt.Errorf("%s: upstream now lists %d legal entries, the manifest records %d:\n%s\n%s",
			a.Name, len(upstream.Legal), len(a.Legal), renderLegal(a.Legal), renderUpstream(upstream))
	}
	for i, want := range a.Legal {
		got := upstream.Legal[i]
		if got.SPDX == want.SPDX && got.What == want.What && got.Artist == want.Artist &&
			got.Owner == want.Owner && got.Year == want.Year && got.Text == want.Text {
			continue
		}
		return fmt.Errorf("%s: legal entry %d has moved upstream\n  manifest: %s\n  upstream: %s",
			a.Name, i,
			legalLine(want.SPDX, want.What, want.Artist, want.Owner, want.Year, want.Text),
			legalLine(got.SPDX, got.What, got.Artist, got.Owner, got.Year, got.Text))
	}
	return nil
}

func legalLine(spdx, what, artist, owner, year, text string) string {
	if spdx == "" {
		spdx = "(legal mark)"
	}
	return fmt.Sprintf("%s %q by %q owned by %q, %s, %q", spdx, what, artist, owner, year, text)
}

func renderLegal(entries []legalEntry) string {
	var out strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&out, "  manifest: %s\n", legalLine(e.SPDX, e.What, e.Artist, e.Owner, e.Year, e.Text))
	}
	return strings.TrimRight(out.String(), "\n")
}

func renderUpstream(m upstreamMetadata) string {
	var out strings.Builder
	for _, e := range m.Legal {
		fmt.Fprintf(&out, "  upstream: %s\n", legalLine(e.SPDX, e.What, e.Artist, e.Owner, e.Year, e.Text))
	}
	return strings.TrimRight(out.String(), "\n")
}
