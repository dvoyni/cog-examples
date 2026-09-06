package main

import (
	"strings"
	"testing"
)

// TestCC0OwesNoAttributionLine keeps the obligations legible. Crediting a CC0
// asset as if it were required makes it impossible to see, in a file of
// fourteen entries, which credits actually have to ship.
func TestCC0OwesNoAttributionLine(t *testing.T) {
	waterBottle, _ := find(manifest, "WaterBottle")
	if lines := attributionLines(waterBottle); len(lines) != 0 {
		t.Errorf("WaterBottle is CC0 and owes %d lines: %v", len(lines), lines)
	}
}

func TestCCBYOwesOneLinePerLicensedPart(t *testing.T) {
	fox, _ := find(manifest, "Fox")
	lines := attributionLines(fox)
	if len(lines) != 2 {
		t.Fatalf("Fox owes %d lines, want 2 - its model is CC0 but its rigging and its conversion are not: %v", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"tomkranis", "@AsoboStudio and @scurest", "Rigging & Animation", "Conversion to glTF"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no mention of %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "PixelMannen") {
		t.Error("PixelMannen's part is CC0 and should not appear as an obligation")
	}
}

// TestEveryAttributionLineSaysItIsModified is the CC-BY-4.0 term this
// distribution is most likely to breach quietly: every asset here is repacked,
// and two are resampled.
func TestEveryAttributionLineSaysItIsModified(t *testing.T) {
	for _, a := range manifest {
		for _, line := range attributionLines(a) {
			if !strings.Contains(line, "modified") {
				t.Errorf("%s: %q does not indicate modification", a.Name, line)
			}
		}
	}
}

func TestModificationsNameTheResampling(t *testing.T) {
	waterBottle, _ := find(manifest, "WaterBottle")
	if got := modifications(waterBottle); !strings.Contains(got, "1024 px") {
		t.Errorf("WaterBottle is resampled, and its modification note does not say so: %q", got)
	}
	fox, _ := find(manifest, "Fox")
	if got := modifications(fox); strings.Contains(got, "resampled") {
		t.Errorf("Fox is vendored byte for byte, and its modification note claims otherwise: %q", got)
	}
}

func TestRenderedAttributionCarriesEveryMark(t *testing.T) {
	rendered := renderAttribution(manifest)
	for _, want := range []string{"Cesium Trademark or Logo", "Khronos Trademark or Logo", "Reserves the mark, not the licence"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("no mention of %q", want)
		}
	}
}

func TestRenderedAttributionCreditsEveryLicensedPart(t *testing.T) {
	rendered := renderAttribution(manifest)
	for _, a := range manifest {
		for _, line := range attributionLines(a) {
			if !strings.Contains(rendered, line) {
				t.Errorf("%s: required line missing from the rendered file: %s", a.Name, line)
			}
		}
	}
}
