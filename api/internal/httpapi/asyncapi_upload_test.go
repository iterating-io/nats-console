package httpapi

import (
	"strings"
	"testing"
)

func TestParseUploadedAsyncAPI(t *testing.T) {
	document := `asyncapi: 3.1.0
servers:
  nats:
    host: example:4222
    x-nats-account: orders
channels:
  Created:
    address: orders.created
    x-jetstream:
      stream: ORDERS
  Copied:
    address: orders.copied
    x-jetstream:
      stream: COPIES
    x-source:
      account: source
      stream: SOURCE
      filterSubject: orders.*
operations:
  sendCreated:
    action: send
    channel:
      $ref: '#/channels/Created'
  receiveCopied:
    action: receive
    channel:
      $ref: '#/channels/Copied'
    x-jetstream-consumer:
      name: copier
`
	channels, err := parseUploadedAsyncAPI([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 || channels[0].Stream != "COPIES" || channels[0].Consumer != "copier" || channels[0].SourceFilter != "orders.*" || channels[1].Stream != "ORDERS" || !channels[1].PublishRequired {
		t.Fatalf("unexpected channels: %#v", channels)
	}
}

func TestParseUploadedAsyncAPIRejectsIncompleteSource(t *testing.T) {
	document := `servers:
  nats:
    x-nats-account: orders
channels:
  Copied:
    address: orders.copied
    x-jetstream:
      stream: COPIES
    x-source:
      account: source
`
	_, err := parseUploadedAsyncAPI([]byte(document))
	if err == nil || !strings.Contains(err.Error(), "incomplete x-source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAsyncAPIReferencesInFolder(t *testing.T) {
	files := map[string][]byte{
		"asyncapi/blog.yaml":             []byte("asyncapi: 3.0.0\nchannels:\n  created:\n    messages:\n      created:\n        $ref: './messages/Created.yaml#/components/messages/Created'\n"),
		"asyncapi/messages/Created.yaml": []byte("components:\n  messages:\n    Created:\n      payload:\n        $ref: '../schemas/Payload.yaml#/components/schemas/Payload'\n"),
		"asyncapi/schemas/Payload.yaml":  []byte("components:\n  schemas:\n    Payload:\n      type: object\n"),
	}
	if err := validateAsyncAPIReferences("asyncapi/blog.yaml", files, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	files["asyncapi/schemas/Payload.yaml"] = []byte("components:\n  schemas:\n    Different:\n      type: object\n")
	if err := validateAsyncAPIReferences("asyncapi/blog.yaml", files, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "missing referenced location") {
		t.Fatalf("expected missing schema location, got %v", err)
	}
	delete(files, "asyncapi/schemas/Payload.yaml")
	if err := validateAsyncAPIReferences("asyncapi/blog.yaml", files, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "missing referenced file") {
		t.Fatalf("expected missing schema error, got %v", err)
	}
}

func TestValidAsyncAPIPath(t *testing.T) {
	for _, name := range []string{"../secret.yaml", "/tmp/file.yaml", "asyncapi/../file.yaml", "asyncapi\\file.yaml"} {
		if validAsyncAPIPath(name) {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
	if !validAsyncAPIPath("asyncapi/messages/Created.yaml") {
		t.Fatal("rejected valid nested path")
	}
}
