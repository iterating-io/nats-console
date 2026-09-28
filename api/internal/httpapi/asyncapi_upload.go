package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v2"

	"github.com/iterating-io/nats-console/api/internal/accounts"
)

const maxAsyncAPIUploadBytes = 2 << 20

type uploadedAsyncAPIFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type uploadCheck struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type uploadChannelResult struct {
	Account string        `json:"account"`
	Stream  string        `json:"stream"`
	Subject string        `json:"subject"`
	Checks  []uploadCheck `json:"checks"`
}

type uploadFileResult struct {
	Name     string                `json:"name"`
	Error    string                `json:"error,omitempty"`
	Channels []uploadChannelResult `json:"channels"`
}

func (s *Server) checkAsyncAPIUploads(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAsyncAPIUploadBytes)
	defer r.Body.Close()
	var request struct {
		Files []uploadedAsyncAPIFile `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Files) == 0 || len(request.Files) > 20 {
		http.Error(w, "expected 1 to 20 YAML files within 2 MiB", http.StatusBadRequest)
		return
	}
	files := make(map[string][]byte, len(request.Files))
	totalBytes := 0
	for _, file := range request.Files {
		if !validAsyncAPIPath(file.Name) {
			http.Error(w, "file names must be relative YAML paths", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(strings.ToLower(file.Name), ".yaml") && !strings.HasSuffix(strings.ToLower(file.Name), ".yml") {
			http.Error(w, "file names must be relative YAML paths", http.StatusBadRequest)
			return
		}
		if _, exists := files[file.Name]; exists {
			http.Error(w, "duplicate AsyncAPI file path", http.StatusBadRequest)
			return
		}
		data := []byte(file.Content)
		totalBytes += len(data)
		if totalBytes > maxAsyncAPIUploadBytes {
			http.Error(w, "expected 1 to 20 YAML files within 2 MiB", http.StatusBadRequest)
			return
		}
		files[file.Name] = data
	}
	results := make([]uploadFileResult, 0, len(request.Files))
	totalChannels := 0
	for _, file := range request.Files {
		var header struct {
			AsyncAPI string `yaml:"asyncapi"`
		}
		if err := yaml.Unmarshal([]byte(file.Content), &header); err != nil {
			results = append(results, uploadFileResult{Name: file.Name, Error: fmt.Sprintf("invalid YAML: %v", err), Channels: []uploadChannelResult{}})
			continue
		}
		if header.AsyncAPI == "" || isReferencedAsyncAPIFile(file.Name, files) {
			continue // messages and schemas are checked through the root document's references
		}
		result := uploadFileResult{Name: file.Name, Channels: []uploadChannelResult{}}
		if err := validateAsyncAPIReferences(file.Name, files, map[string]bool{}); err != nil {
			result.Error = err.Error()
		} else {
			channels, err := parseUploadedAsyncAPI([]byte(file.Content))
			if err != nil {
				result.Error = err.Error()
			} else {
				totalChannels += len(channels)
				if totalChannels > 100 {
					http.Error(w, "at most 100 channels can be checked", http.StatusBadRequest)
					return
				}
				for _, channel := range channels {
					item := uploadChannelResult{Account: channel.Account, Stream: channel.Stream, Subject: channel.Subject, Checks: []uploadCheck{}}
					status, checkErr := s.accountsService.CheckAsyncAPIChannel(channel)
					if checkErr != nil {
						result.Error = checkErr.Error()
						break
					}
					item.Checks = evaluateUploadedChannel(channel, status)
					result.Channels = append(result.Channels, item)
				}
			}
		}
		results = append(results, result)
	}
	if len(results) == 0 {
		http.Error(w, "no root AsyncAPI document found", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"files": results})
}

func isReferencedAsyncAPIFile(name string, files map[string][]byte) bool {
	for root, data := range files {
		if root == name {
			continue
		}
		var document interface{}
		if yaml.Unmarshal(data, &document) != nil {
			continue
		}
		if referencesFile(document, root, name) {
			return true
		}
	}
	return false
}

func referencesFile(document interface{}, source, target string) bool {
	switch item := document.(type) {
	case map[interface{}]interface{}:
		for key, child := range item {
			if key == "$ref" {
				ref, ok := child.(string)
				if ok {
					file := strings.SplitN(ref, "#", 2)[0]
					if file != "" && path.Clean(path.Join(path.Dir(source), file)) == target {
						return true
					}
				}
			}
			if referencesFile(child, source, target) {
				return true
			}
		}
	case []interface{}:
		for _, child := range item {
			if referencesFile(child, source, target) {
				return true
			}
		}
	}
	return false
}

func validAsyncAPIPath(name string) bool {
	return name != "" && !strings.Contains(name, "\\") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../") && (strings.HasSuffix(strings.ToLower(name), ".yaml") || strings.HasSuffix(strings.ToLower(name), ".yml"))
}

func validateAsyncAPIReferences(name string, files map[string][]byte, visited map[string]bool) error {
	if visited[name] {
		return nil
	}
	visited[name] = true
	var document interface{}
	if err := yaml.Unmarshal(files[name], &document); err != nil {
		return fmt.Errorf("%s: invalid YAML: %w", name, err)
	}
	var walk func(interface{}) error
	walk = func(value interface{}) error {
		switch item := value.(type) {
		case map[interface{}]interface{}:
			for key, child := range item {
				if key == "$ref" {
					ref, ok := child.(string)
					if !ok {
						return fmt.Errorf("%s: $ref must be a string", name)
					}
					parts := strings.SplitN(ref, "#", 2)
					target := name
					if parts[0] != "" {
						target = path.Clean(path.Join(path.Dir(name), parts[0]))
					}
					if !validAsyncAPIPath(target) || ref == "" {
						return fmt.Errorf("%s: invalid relative $ref %q", name, ref)
					}
					data, found := files[target]
					if !found {
						return fmt.Errorf("%s: missing referenced file %s", name, target)
					}
					if len(parts) == 2 && !yamlPointerExists(data, parts[1]) {
						return fmt.Errorf("%s: missing referenced location %s", name, ref)
					}
					if err := validateAsyncAPIReferences(target, files, visited); err != nil {
						return err
					}
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range item {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(document)
}

func yamlPointerExists(data []byte, pointer string) bool {
	var value interface{}
	if err := yaml.Unmarshal(data, &value); err != nil {
		return false
	}
	if pointer == "" {
		return true
	}
	if !strings.HasPrefix(pointer, "/") {
		return false
	}
	for _, segment := range strings.Split(pointer[1:], "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		switch node := value.(type) {
		case map[interface{}]interface{}:
			child, ok := node[segment]
			if !ok {
				return false
			}
			value = child
		case []interface{}:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return false
			}
			value = node[index]
		default:
			return false
		}
	}
	return true
}

func parseUploadedAsyncAPI(data []byte) ([]accounts.CheckerChannelRequest, error) {
	var document struct {
		Servers map[string]struct {
			Account string `yaml:"x-nats-account"`
		} `yaml:"servers"`
		Operations map[string]struct {
			Action  string `yaml:"action"`
			Channel struct {
				Ref string `yaml:"$ref"`
			} `yaml:"channel"`
			Consumer struct {
				Name string `yaml:"name"`
			} `yaml:"x-jetstream-consumer"`
		} `yaml:"operations"`
		Channels map[string]struct {
			Address   string `yaml:"address"`
			JetStream *struct {
				Stream string `yaml:"stream"`
			} `yaml:"x-jetstream"`
			Source struct {
				Account string `yaml:"account"`
				Stream  string `yaml:"stream"`
				Filter  string `yaml:"filterSubject"`
			} `yaml:"x-source"`
		} `yaml:"channels"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("invalid AsyncAPI YAML: %w", err)
	}
	accountNames := map[string]bool{}
	for _, server := range document.Servers {
		if server.Account != "" {
			accountNames[server.Account] = true
		}
	}
	if len(accountNames) != 1 {
		return nil, fmt.Errorf("expected exactly one x-nats-account")
	}
	account := ""
	for name := range accountNames {
		account = name
	}
	sends := map[string]bool{}
	consumers := map[string]string{}
	for name, operation := range document.Operations {
		if operation.Action != "send" && operation.Action != "receive" {
			continue
		}
		if !strings.HasPrefix(operation.Channel.Ref, "#/channels/") || strings.TrimPrefix(operation.Channel.Ref, "#/channels/") == "" {
			return nil, fmt.Errorf("operation %q must reference a local channel", name)
		}
		channelName := strings.TrimPrefix(operation.Channel.Ref, "#/channels/")
		if _, ok := document.Channels[channelName]; !ok {
			return nil, fmt.Errorf("operation %q references an unknown channel", name)
		}
		if operation.Action == "send" {
			sends[channelName] = true
		} else {
			if operation.Consumer.Name == "" {
				return nil, fmt.Errorf("receive operation %q has no x-jetstream-consumer.name", name)
			}
			if previous := consumers[channelName]; previous != "" && previous != operation.Consumer.Name {
				return nil, fmt.Errorf("channel %q declares conflicting Consumer names", channelName)
			}
			consumers[channelName] = operation.Consumer.Name
		}
	}
	names := make([]string, 0, len(document.Channels))
	for name := range document.Channels {
		names = append(names, name)
	}
	sort.Strings(names)
	channels := make([]accounts.CheckerChannelRequest, 0, len(names))
	for _, name := range names {
		value := document.Channels[name]
		if value.JetStream == nil {
			continue
		}
		if value.Address == "" || value.JetStream.Stream == "" {
			return nil, fmt.Errorf("channel %q requires address and x-jetstream.stream", name)
		}
		if (value.Source.Account == "") != (value.Source.Stream == "") || value.Source.Account != "" && value.Source.Filter == "" || value.Source.Account == "" && value.Source.Filter != "" {
			return nil, fmt.Errorf("channel %q has an incomplete x-source", name)
		}
		channels = append(channels, accounts.CheckerChannelRequest{Account: account, Stream: value.JetStream.Stream, Subject: value.Address, PublishRequired: sends[name], Consumer: consumers[name], SourceAccount: value.Source.Account, SourceStream: value.Source.Stream, SourceFilter: value.Source.Filter})
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("no JetStream channels")
	}
	return channels, nil
}

func evaluateUploadedChannel(request accounts.CheckerChannelRequest, status accounts.CheckerChannelStatus) []uploadCheck {
	checks := []uploadCheck{}
	add := func(name, target string, passed bool, detail string) {
		checks = append(checks, uploadCheck{Name: name, Target: target, Passed: passed, Detail: detail})
	}
	add("Account exists", request.Account, status.Account.Exists, "Create the service account in NATS Console.")
	if !status.Account.Exists {
		return checks
	}
	add("JetStream enabled", request.Account, status.Account.JetStreamEnabled, "Enable JetStream for the account.")
	if request.PublishRequired {
		add("Publish allowed", request.Subject, status.Account.PublishAllowed != nil && *status.Account.PublishAllowed, "Allow the subject in account publish permissions.")
	}
	add("Stream exists", request.Stream, status.TargetStream, status.TargetStreamError)
	if !status.TargetStream {
		return checks
	}
	if request.SourceAccount == "" {
		add("Stream covers subject", request.Subject, status.SubjectCovered != nil && *status.SubjectCovered, "Add the subject to the stream.")
	} else {
		source := status.SourceAccount
		add("Source account exists", request.SourceAccount, source != nil && source.Exists, "Create the source account.")
		add("Source JetStream enabled", request.SourceAccount, source != nil && source.JetStreamEnabled, "Enable JetStream for the source account.")
		add("Source stream exists", request.SourceStream, status.SourceStream != nil && *status.SourceStream, status.SourceStreamError)
		sharing := source != nil && source.SourceSharing != nil
		add("Source sharing enabled", request.SourceAccount, sharing && source.SourceSharing.SourceSharingEnabled, "Enable Stream Source sharing.")
		add("Source account connection", request.SourceAccount+" → "+request.Account, sharing && source.SourceSharing.SourceExportsEnabled && source.SourceSharing.ConsumerAPIImportEnabled && source.SourceSharing.DeliverySubjectImportEnabled && source.SourceSharing.FlowControlImportEnabled, "Update the source account connection in NATS Console.")
		add("Source connected", request.SourceStream+" → "+request.Stream, status.SourceConnected != nil && *status.SourceConnected, "Connect the source stream.")
		add("Source filter", request.SourceFilter, status.SourceFilter != nil && *status.SourceFilter, "Set the source filter subject.")
	}
	if request.Consumer != "" {
		add("Consumer ready", request.Consumer, status.ConsumerReady != nil && *status.ConsumerReady, status.ConsumerError)
	}
	for i := range checks {
		if checks[i].Passed {
			checks[i].Detail = ""
		}
	}
	return checks
}
