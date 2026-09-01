package accounts

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestRemoveExternalAccountSources(t *testing.T) {
	deleted := "ADELETED"
	keep := &nats.StreamSource{Name: "KEEP", External: &nats.ExternalStream{APIPrefix: sourceAPIPrefix("AOTHER")}}
	removeOne := &nats.StreamSource{Name: "REMOVE", FilterSubject: "events.one", External: &nats.ExternalStream{APIPrefix: sourceAPIPrefix(deleted)}}
	removeTwo := &nats.StreamSource{Name: "REMOVE", FilterSubject: "events.two", External: &nats.ExternalStream{APIPrefix: sourceAPIPrefix(deleted)}}
	cfg := nats.StreamConfig{
		Name:    "TARGET",
		Sources: []*nats.StreamSource{keep, removeOne, removeTwo},
		Mirror:  &nats.StreamSource{Name: "MIRROR", External: &nats.ExternalStream{APIPrefix: sourceAPIPrefix(deleted)}},
	}

	updated, changed := removeExternalAccountSources(cfg, deleted)
	if !changed {
		t.Fatal("removeExternalAccountSources() changed = false, want true")
	}
	if len(updated.Sources) != 1 || updated.Sources[0] != keep {
		t.Fatalf("sources = %#v, want only unrelated source", updated.Sources)
	}
	if updated.Mirror != nil {
		t.Fatalf("mirror = %#v, want nil", updated.Mirror)
	}
}

func TestRemoveExternalAccountSourcesLeavesUnrelatedConfig(t *testing.T) {
	source := &nats.StreamSource{Name: "KEEP", External: &nats.ExternalStream{APIPrefix: sourceAPIPrefix("AOTHER")}}
	cfg := nats.StreamConfig{Name: "TARGET", Sources: []*nats.StreamSource{source}}

	updated, changed := removeExternalAccountSources(cfg, "ADELETED")
	if changed {
		t.Fatal("removeExternalAccountSources() changed = true, want false")
	}
	if len(updated.Sources) != 1 || updated.Sources[0] != source {
		t.Fatalf("sources = %#v, want original source", updated.Sources)
	}
}
