package accounts

import (
	"strings"
	"sync"
	"time"
)

type OperatorRecord struct {
	Name string `json:"name"`
}

type Record struct {
	Name           string   `json:"name"`
	Operator       string   `json:"operator"`
	PublishAllow   []string `json:"publishAllow"`
	SubscribeAllow []string `json:"subscribeAllow"`
	PublicKey      string   `json:"publicKey"`
	IsSystem       bool     `json:"isSystem"`
	JSEnabled      bool     `json:"jsEnabled"`
}

type Capabilities struct {
	AccountDelete bool `json:"accountDelete"`
}

type Repository struct {
	mu                    sync.RWMutex
	operators             []OperatorRecord
	accounts              []Record
	capabilities          Capabilities
	capabilitiesCheckedAt time.Time
}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) ListOperators() []OperatorRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.operators == nil {
		return []OperatorRecord{}
	}
	return append([]OperatorRecord{}, r.operators...)
}

func (r *Repository) ListAccounts() []Record {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Record, 0, len(r.accounts))
	for _, acc := range r.accounts {
		if !acc.IsSystem {
			list = append(list, acc)
		}
	}
	return list
}

func (r *Repository) OperatorExists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, op := range r.operators {
		if op.Name == name {
			return true
		}
	}
	return false
}

func (r *Repository) AccountNameExists(operator, name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, acc := range r.accounts {
		if acc.Operator == operator && strings.EqualFold(acc.Name, name) {
			return true
		}
	}
	return false
}

func (r *Repository) AddAccount(record Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accounts = append(r.accounts, record)
}

func (r *Repository) RemoveAccount(operator, publicKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := r.accounts[:0]
	for _, acc := range r.accounts {
		if !(acc.Operator == operator && acc.PublicKey == publicKey) {
			filtered = append(filtered, acc)
		}
	}
	r.accounts = filtered
}

func (r *Repository) FindByPublicKey(operator, publicKey string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, acc := range r.accounts {
		if acc.Operator == operator && acc.PublicKey == publicKey {
			return acc, true
		}
	}
	return Record{}, false
}

func (r *Repository) FindAnyByPublicKey(publicKey string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, acc := range r.accounts {
		if acc.PublicKey == publicKey {
			return acc, true
		}
	}
	return Record{}, false
}

func (r *Repository) FindByName(operator, name string) (Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, acc := range r.accounts {
		if acc.Operator == operator && acc.Name == name {
			return acc, true
		}
	}
	return Record{}, false
}

func (r *Repository) UpdateJetStream(operator, publicKey string, enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, acc := range r.accounts {
		if acc.Operator == operator && acc.PublicKey == publicKey {
			r.accounts[i].JSEnabled = enabled
			return
		}
	}
}

func (r *Repository) AddPublishAllow(operator, name, subject string) (Record, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, acc := range r.accounts {
		if acc.Operator == operator && acc.Name == name {
			for _, existing := range acc.PublishAllow {
				if existing == subject {
					return Record{}, true, true
				}
			}
			r.accounts[i].PublishAllow = append(r.accounts[i].PublishAllow, subject)
			return r.accounts[i], true, false
		}
	}
	return Record{}, false, false
}

func (r *Repository) RemovePublishAllow(operator, name, subject string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, acc := range r.accounts {
		if acc.Operator == operator && acc.Name == name {
			filtered := acc.PublishAllow[:0]
			for _, existing := range acc.PublishAllow {
				if existing != subject {
					filtered = append(filtered, existing)
				}
			}
			r.accounts[i].PublishAllow = filtered
			return r.accounts[i], true
		}
	}
	return Record{}, false
}

func (r *Repository) AddSubscribeAllow(operator, name, subject string) (Record, bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, acc := range r.accounts {
		if acc.Operator == operator && acc.Name == name {
			for _, existing := range acc.SubscribeAllow {
				if existing == subject {
					return Record{}, true, true
				}
			}
			r.accounts[i].SubscribeAllow = append(r.accounts[i].SubscribeAllow, subject)
			return r.accounts[i], true, false
		}
	}
	return Record{}, false, false
}

func (r *Repository) RemoveSubscribeAllow(operator, name, subject string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, acc := range r.accounts {
		if acc.Operator == operator && acc.Name == name {
			filtered := acc.SubscribeAllow[:0]
			for _, existing := range acc.SubscribeAllow {
				if existing != subject {
					filtered = append(filtered, existing)
				}
			}
			r.accounts[i].SubscribeAllow = filtered
			return r.accounts[i], true
		}
	}
	return Record{}, false
}

func (r *Repository) Capabilities() Capabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilities
}

func (r *Repository) CapabilitiesCheckedAt() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.capabilitiesCheckedAt
}

func (r *Repository) SetCapabilities(capabilities Capabilities, checkedAt time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities = capabilities
	r.capabilitiesCheckedAt = checkedAt
}

func (r *Repository) EnsureOperator(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, op := range r.operators {
		if op.Name == name {
			return
		}
	}
	r.operators = append(r.operators, OperatorRecord{Name: name})
}

func (r *Repository) UpsertLoaded(loaded []Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := map[string]int{}
	for i, acc := range r.accounts {
		existing[acc.Operator+"/"+acc.Name] = i
	}
	for _, acc := range loaded {
		key := acc.Operator + "/" + acc.Name
		if idx, found := existing[key]; found {
			r.accounts[idx].PublishAllow = acc.PublishAllow
			r.accounts[idx].SubscribeAllow = acc.SubscribeAllow
			r.accounts[idx].JSEnabled = acc.JSEnabled
			continue
		}
		r.accounts = append(r.accounts, acc)
	}
}
