package jetstream

import (
	"testing"
	"time"

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

func TestUpdateStreamSourceFiltersPreservesSourceConfiguration(t *testing.T) {
	existing := &nats.StreamSource{
		Name:          "ORDERS",
		FilterSubject: "orders.created",
		External:      &nats.ExternalStream{APIPrefix: "$JS.SOURCE.A.API", DeliverPrefix: "$JS.SOURCE.C"},
	}
	updated, err := updateStreamSourceFilters(
		[]*nats.StreamSource{existing},
		&nats.StreamSource{Name: "ORDERS", External: &nats.ExternalStream{APIPrefix: "$JS.SOURCE.A.API", DeliverPrefix: "$JS.SOURCE.C"}},
		"orders.created",
		[]string{"orders.updated", "orders.cancelled"},
	)
	if err != nil {
		t.Fatalf("updateStreamSourceFilters() error = %v", err)
	}
	if len(updated) != 2 || updated[0].FilterSubject != "orders.updated" || updated[1].FilterSubject != "orders.cancelled" || updated[0].External != existing.External || updated[1].External != existing.External {
		t.Fatalf("updateStreamSourceFilters() = %#v, want two updated filters with preserved external config", updated)
	}
	if existing.FilterSubject != "orders.created" {
		t.Fatalf("updateStreamSourceFilters() mutated existing source: %#v", existing)
	}
}

func TestUpdateStreamSourceFiltersRejectsUnknownSource(t *testing.T) {
	_, err := updateStreamSourceFilters([]*nats.StreamSource{{Name: "ORDERS"}}, &nats.StreamSource{Name: "PAYMENTS"}, "", []string{"payments.>"})
	if err == nil {
		t.Fatal("updateStreamSourceFilters() error = nil, want unknown source error")
	}
}

func TestStreamSourceFilters(t *testing.T) {
	filters := streamSourceFilters(" orders.created, orders.updated, orders.created, ")
	if len(filters) != 2 || filters[0] != "orders.created" || filters[1] != "orders.updated" {
		t.Fatalf("streamSourceFilters() = %#v, want unique trimmed filters", filters)
	}
}

func TestRemoveStreamSource(t *testing.T) {
	source := &nats.StreamSource{Name: "ORDERS"}
	updated, err := removeStreamSource([]*nats.StreamSource{
		{Name: "ORDERS", FilterSubject: "orders.created"},
		{Name: "ORDERS", FilterSubject: "orders.updated"},
		{Name: "PAYMENTS", FilterSubject: "payments.created"},
	}, source)
	if err != nil {
		t.Fatalf("removeStreamSource() error = %v", err)
	}
	if len(updated) != 1 || updated[0].Name != "PAYMENTS" {
		t.Fatalf("removeStreamSource() = %#v, want only PAYMENTS", updated)
	}
}

func TestRemoveStreamSourceRejectsUnknownSource(t *testing.T) {
	_, err := removeStreamSource([]*nats.StreamSource{{Name: "ORDERS"}}, &nats.StreamSource{Name: "PAYMENTS"})
	if err == nil {
		t.Fatal("removeStreamSource() error = nil, want unknown source error")
	}
}

func TestParseConsumerOperationalSettings(t *testing.T) {
	ackWait, maxDeliver, maxAckPending, err := parseConsumerOperationalSettings("", 0, 0)
	if err != nil {
		t.Fatalf("defaults returned error: %v", err)
	}
	if ackWait != 30*time.Second || maxDeliver != -1 || maxAckPending != 1000 {
		t.Fatalf("defaults = %v, %d, %d", ackWait, maxDeliver, maxAckPending)
	}
	if _, _, _, err := parseConsumerOperationalSettings("bad", 5, 10); err == nil {
		t.Fatal("invalid ackWait should fail")
	}
	if _, _, _, err := parseConsumerOperationalSettings("10s", -2, 10); err == nil {
		t.Fatal("maxDeliver below -1 should fail")
	}
}

func TestRemoveStreamSourceFilter(t *testing.T) {
	source := &nats.StreamSource{Name: "ORDERS"}
	updated, err := removeStreamSourceFilter([]*nats.StreamSource{
		{Name: "ORDERS", FilterSubject: "orders.created"},
		{Name: "ORDERS", FilterSubject: "orders.updated"},
	}, source, "orders.created")
	if err != nil {
		t.Fatalf("removeStreamSourceFilter() error = %v", err)
	}
	if len(updated) != 1 || updated[0].FilterSubject != "orders.updated" {
		t.Fatalf("removeStreamSourceFilter() = %#v, want remaining filter", updated)
	}
}
