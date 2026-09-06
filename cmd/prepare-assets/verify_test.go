package main

import (
	"strings"
	"testing"
)

const waterBottleMetadata = `{
  "version": 2,
  "legal": [
    {
      "license": "CC0-1.0",
      "artist": "Microsoft",
      "year": "2017",
      "owner": "Public",
      "what": "Everything",
      "text": "Creative Commons Zero v1.0 Universal",
      "spdx": "CC0-1.0"
    }
  ],
  "name": "Water Bottle"
}`

func waterBottle(t *testing.T) asset {
	t.Helper()
	a, ok := find(manifest, "WaterBottle")
	if !ok {
		t.Fatal("WaterBottle left the manifest")
	}
	return a
}

func TestVerifyLegalAcceptsUnchangedTerms(t *testing.T) {
	if err := verifyLegal(waterBottle(t), []byte(waterBottleMetadata)); err != nil {
		t.Fatal(err)
	}
}

// TestVerifyLegalCatchesARelicensing is the reason the terms are recorded by
// hand rather than read out of the asset. A table that copies its own licence
// from the thing it licenses can never notice this.
func TestVerifyLegalCatchesARelicensing(t *testing.T) {
	relicensed := strings.ReplaceAll(waterBottleMetadata, `"spdx": "CC0-1.0"`, `"spdx": "CC-BY-NC-4.0"`)
	err := verifyLegal(waterBottle(t), []byte(relicensed))
	if err == nil {
		t.Fatal("a relicensing passed")
	}
	if !strings.Contains(err.Error(), "CC-BY-NC-4.0") {
		t.Errorf("the error should show what upstream now says: %v", err)
	}
}

// TestVerifyLegalCatchesANewMark is whole-record on purpose: a trademark
// reservation appearing upstream is as much a reason to stop and look as a
// changed licence identifier.
func TestVerifyLegalCatchesANewMark(t *testing.T) {
	grown := strings.Replace(waterBottleMetadata, `"legal": [`, `"legal": [
    { "spdx": "", "what": "A logo", "artist": "Non-copyrightable logo", "owner": "Someone", "year": "2026", "text": "Some Trademark or Logo" },`, 1)
	if err := verifyLegal(waterBottle(t), []byte(grown)); err == nil {
		t.Fatal("a new legal mark passed")
	}
}

func TestVerifyLegalCatchesAChangedAttributionTarget(t *testing.T) {
	moved := strings.ReplaceAll(waterBottleMetadata, `"artist": "Microsoft"`, `"artist": "Somebody Else"`)
	if err := verifyLegal(waterBottle(t), []byte(moved)); err == nil {
		t.Fatal("a changed artist passed - that is who an attribution line has to name")
	}
}

func TestVerifyLegalRejectsUnreadableMetadata(t *testing.T) {
	if err := verifyLegal(waterBottle(t), []byte("{oops")); err == nil {
		t.Fatal("unreadable metadata passed")
	}
}
