package app

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestPublishedStorePoliciesExcludeEveryDraftField(t *testing.T) {
	at := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	raw := marshal(map[string]storePolicyRecord{
		"terms":   {Draft: PolicyContent{"PRIVATE_TITLE", "PRIVATE_BODY"}, Version: 2, Published: &PolicyContent{"Terms", "Public body"}, PublishedVersion: 1, PublishedAt: &at},
		"privacy": {Draft: PolicyContent{"Private privacy", "PRIVATE_BODY"}, Version: 1},
	})
	views, err := publishedStorePolicies(raw)
	if err != nil || len(views) != 1 || views[0].Title != "Terms" || views[0].Version != 1 {
		t.Fatal(views, err)
	}
	encoded, _ := json.Marshal(views)
	for _, private := range [][]byte{[]byte("PRIVATE"), []byte("draft"), []byte("updated_at"), []byte("published_version")} {
		if bytes.Contains(encoded, private) {
			t.Fatal("draft state leaked into public policy payload", string(encoded))
		}
	}
}

func TestLegacyStoreSettingsHaveThreeEmptyPolicyDrafts(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null"), []byte("{}")} {
		rows, err := readStorePolicies(raw)
		if err != nil || len(rows) != 3 {
			t.Fatal(rows, err)
		}
		for _, key := range storePolicyKeys {
			if rows[key].Draft.Title != storePolicyTitle(key) || rows[key].Draft.Body != "" || rows[key].Published != nil {
				t.Fatal("legacy policy silently received a public document", rows[key])
			}
		}
	}
	if _, err := publishedStorePolicies([]byte(`{"terms":"invalid"}`)); err == nil {
		t.Fatal("invalid stored policy state was accepted")
	}
}
