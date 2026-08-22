package jetstream

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestStreamSources(t *testing.T) {
	sources, err := streamSources([]string{" orders ", "payments", "orders", ""}, "events")
	if err != nil {
		t.Fatalf("streamSources() error = %v", err)
	}
	if len(sources) != 2 || sources[0].Name != "orders" || sources[1].Name != "payments" {
		t.Fatalf("streamSources() = %#v, want orders and payments", sources)
	}
}

func TestStreamSourcesRejectsSelf(t *testing.T) {
	if _, err := streamSources([]string{"events"}, "events"); err == nil {
		t.Fatal("streamSources() error = nil, want self-source error")
	}
}

func TestAppendStreamSourcePreservesExistingSources(t *testing.T) {
	existing := &nats.StreamSource{
		Name:     "ORDERS",
		External: &nats.ExternalStream{APIPrefix: "$JS.SOURCE.A.API", DeliverPrefix: "$JS.SOURCE.C"},
	}
	additional := &nats.StreamSource{
		Name:     "PAYMENTS",
		External: &nats.ExternalStream{APIPrefix: "$JS.SOURCE.B.API", DeliverPrefix: "$JS.SOURCE.C"},
	}

	sources, err := appendStreamSource([]*nats.StreamSource{existing}, additional)
	if err != nil {
		t.Fatalf("appendStreamSource() error = %v", err)
	}
	if len(sources) != 2 || sources[0] != existing || sources[1] != additional {
		t.Fatalf("appendStreamSource() = %#v, want both original and additional source", sources)
	}
}

func TestAppendStreamSourceRejectsDuplicate(t *testing.T) {
	source := &nats.StreamSource{
		Name:     "ORDERS",
		External: &nats.ExternalStream{APIPrefix: "$JS.SOURCE.A.API", DeliverPrefix: "$JS.SOURCE.C"},
	}
	if _, err := appendStreamSource([]*nats.StreamSource{source}, source); err == nil {
		t.Fatal("appendStreamSource() error = nil, want duplicate error")
	}
}
