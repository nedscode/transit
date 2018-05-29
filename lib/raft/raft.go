package raft

// Based on reference implementation at https://github.com/otoolep/hraftd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/raft"
	"github.com/hashicorp/raft-boltdb"
	"github.com/sirupsen/logrus"

	"github.com/nedscode/transit/lib/secret"
)

const (
	retainSnapshotCount = 2
	raftTimeout         = 3 * time.Second
)

// Command represents a raft command operation.
type Command struct {
	Operation string `json:"op,omitempty"`
	Key       string `json:"key,omitempty"`
	Value     string `json:"val,omitempty"`
	Compare   string `json:"cmp,omitempty"`
	Versus    string `json:"vs,omitempty"`
}

// New returns a new Store.
func New(id, bind, dataDir string, bootstrap bool, logger logrus.FieldLogger) *Store {
	return &Store{
		id:      id,
		bind:    bind,
		boot:    bootstrap,
		dataDir: dataDir,
		m:       map[string]string{},
		logger:  logger,
	}
}

// Store is a state storage for the raft instance.
type Store struct {
	SeedKey string

	id      string
	bind    string
	boot    bool
	dataDir string
	peers   []string
	mu      sync.RWMutex
	m       map[string]string
	raft    *raft.Raft
	logger  logrus.FieldLogger
}

type storer interface {
	raft.LogStore
	raft.StableStore
}

// Start begins the raft server.
func (s *Store) Start(ctx context.Context, persist bool) error {
	notifyChan := make(chan bool, 10)

	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(s.id)
	config.NotifyCh = notifyChan
	address := strings.Split(s.id, ":")[0]

	go func() {
		var peerList string
		tock := time.NewTicker(5 * time.Second)

		for {
			select {
			case leading := <-notifyChan:
				if leading {
					s.logger.Info("Became leader")
				} else {
					s.logger.Info("Following the leader")
				}
			case <-tock.C:
				addrs := s.PeerAddresses()
				sort.Strings(addrs)
				peers := strings.Join(addrs, ",")
				if peers != peerList {
					// Write the new cluster file
					s.logger.WithField("peers", addrs).Info("Peer list has changed")
					cluster := fmt.Sprintf("transit://%s/%s", peers, s.Key())
					ioutil.WriteFile(path.Join(s.dataDir, "cluster"), []byte(cluster), 0600)
					peerList = peers
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	addr, err := net.ResolveTCPAddr("tcp", s.bind)
	if err != nil {
		return err
	}

	transport, err := raft.NewTCPTransport(s.bind, addr, 3, raftTimeout, os.Stderr)
	if err != nil {
		return err
	}

	var snapshots raft.SnapshotStore
	var logStore storer

	if persist {
		snapshots, err = raft.NewFileSnapshotStore(s.dataDir, retainSnapshotCount, os.Stderr)
		if err != nil {
			return fmt.Errorf("file snapshot store: %s", err)
		}

		logStore, err = raftboltdb.NewBoltStore(filepath.Join(s.dataDir, "raft.db"))
		if err != nil {
			return fmt.Errorf("new bolt store: %s", err)
		}
	} else {
		snapshots = raft.NewInmemSnapshotStore()
		logStore = raft.NewInmemStore()
	}

	s.raft, err = raft.NewRaft(config, s, logStore, logStore, snapshots, transport)
	if err != nil {
		return fmt.Errorf("new raft: %s", err)
	}

	if s.boot {
		exist := s.Get("cluster/key")
		if exist != "" {
			return fmt.Errorf("cannot boot, cluster already has key")
		}

		s.logger.WithField("id", config.LocalID).WithField("addr", s.bind).Info("booting")
		// This cluster has not been initialised yet.
		s.raft.BootstrapCluster(raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      config.LocalID,
					Address: raft.ServerAddress(address + s.bind),
				},
			},
		})
	}

	// Forces a wait for next configuration
	s.raft.GetConfiguration().Error()

	if s.boot {
		if s.SeedKey == "" {
			data, fErr := ioutil.ReadFile(filepath.Join(s.dataDir, "cluster-key"))
			if fErr == nil {
				s.logger.Info("Loading previous cluster-key file")
				s.SeedKey = string(data)
			}
		}
		if s.SeedKey == "" {
			s.SeedKey = secret.String(48)
		}

		if len(s.SeedKey) != 48 {
			s.logger.Fatal("Cluster key is incorrect length")
		}

		s.logger.Info("Waiting for leadership...")
		for leading := range s.raft.LeaderCh() {
			if leading {
				s.logger.Info("Got it!")
				break
			}
			s.logger.Info("Still waiting...")
		}

		err = s.Set("cluster/key", s.SeedKey, "", "")
		if err != nil {
			s.logger.WithError(err).Fatal("Failed to set cluster key in raft")
		}

		ioutil.WriteFile(filepath.Join(s.dataDir, "cluster-key"), []byte(s.SeedKey), 0600)
	}

	return nil
}

// Key returns the secret key shared by all servers in the cluster.
// This key must be used to perform cluster level API calls.
func (s *Store) Key() string {
	return s.Get("cluster/key")
}

// List returns any keys with the given prefix string.
func (s *Store) List(prefix string) (list map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list = map[string]string{}
	for k, v := range s.m {
		if strings.HasPrefix(k, prefix) {
			list[k] = v
		}
	}

	return
}

// Get returns the value for the given key.
func (s *Store) Get(key string) (value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value = s.m[key]
	return
}

// Operate directly runs a command in raft.
func (s *Store) Operate(c *Command) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	b, err := json.Marshal(c)
	if err != nil {
		return err
	}

	f := s.raft.Apply(b, raftTimeout)
	return f.Error()
}

// Set sets the value for the given key.
func (s *Store) Set(key, value, cmp, vs string) error {
	return s.Operate(&Command{
		Operation: "set",
		Key:       key,
		Value:     value,
		Compare:   cmp,
		Versus:    vs,
	})
}

// Delete deletes the given key.
func (s *Store) Delete(key, cmp, vs string) error {
	return s.Operate(&Command{
		Operation: "delete",
		Key:       key,
		Compare:   cmp,
		Versus:    vs,
	})
}

// Increment increases the given key's value.
func (s *Store) Increment(key string, by int, cmp, vs string) error {
	return s.Operate(&Command{
		Operation: "increment",
		Key:       key,
		Value:     strconv.Itoa(by),
		Compare:   cmp,
		Versus:    vs,
	})
}

// Decrement decreases the given key's value.
func (s *Store) Decrement(key string, by int, cmp, vs string) error {
	return s.Operate(&Command{
		Operation: "decrement",
		Key:       key,
		Value:     strconv.Itoa(by),
		Compare:   cmp,
		Versus:    vs,
	})
}

// Leader returns the address of the current leader.
func (s *Store) Leader() string {
	if s == nil || s.raft == nil {
		s.logger.Error("Can't get leader address from store")
		return ""
	}

	leader := s.raft.Leader()
	cfg := s.raft.GetConfiguration().Configuration()
	for _, srv := range cfg.Servers {
		if srv.Address == leader {
			return string(srv.ID)
		}
	}
	return ""
}

// PeerAddresses returns the addresses of peers within the cluster.
func (s *Store) PeerAddresses() (peers []string) {
	if s == nil || s.raft == nil {
		s.logger.Error("Can't get peer addresses from store")
		return
	}

	future := s.raft.GetConfiguration()
	if err := future.Error(); err != nil {
		s.logger.WithError(err).Error("Couldn't get raft configuration")
		return
	}

	cfg := future.Configuration()
	for _, srv := range cfg.Servers {
		peers = append(peers, string(srv.ID))
	}
	return
}

// Join joins a node, identified by nodeID and located at addr, to this store.
// The node must be ready to respond to Raft communications at that address.
func (s *Store) Join(nodeID, addr string) error {
	if s.raft.State() != raft.Leader {
		return fmt.Errorf("not leader")
	}

	s.logger.Debugf("Received join request for remote node %s at %s", nodeID, addr)

	configFuture := s.raft.GetConfiguration()
	if err := configFuture.Error(); err != nil {
		s.logger.Debugf("Failed to get raft configuration: %v", err)
		return err
	}

	for _, srv := range configFuture.Configuration().Servers {
		// If a node already exists with either the joining node's ID or address,
		// that node may need to be removed from the config first.
		if srv.ID == raft.ServerID(nodeID) || srv.Address == raft.ServerAddress(addr) {
			// However if *both* the ID and the address are the same, then nothing -- not even
			// a join operation -- is needed.
			if srv.Address == raft.ServerAddress(addr) && srv.ID == raft.ServerID(nodeID) {
				s.logger.Debugf("Node %s at %s already member of cluster, ignoring join request", nodeID, addr)
				return nil
			}

			future := s.raft.RemoveServer(srv.ID, 0, 0)
			if err := future.Error(); err != nil {
				return fmt.Errorf("error removing existing node %s at %s: %s", nodeID, addr, err)
			}
		}
	}

	f := s.raft.AddVoter(raft.ServerID(nodeID), raft.ServerAddress(addr), 0, 0)
	if f.Error() != nil {
		return f.Error()
	}
	s.logger.Debugf("Node %s at %s joined successfully", nodeID, addr)
	return nil
}

// Apply applies a Raft log entry to the key-value store.
func (s *Store) Apply(l *raft.Log) interface{} {
	var c Command
	if err := json.Unmarshal(l.Data, &c); err != nil {
		panic(fmt.Sprintf("failed to unmarshal command: %s", err.Error()))
	}
	return s.applyCommand(c)
}

// Snapshot returns a snapshot of the key-value store.
func (s *Store) Snapshot() (raft.FSMSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clone the map.
	o := make(map[string]string)
	for k, v := range s.m {
		o[k] = v
	}
	return &Snapshot{store: o}, nil
}

// Restore stores the key-value store to a previous state.
func (s *Store) Restore(rc io.ReadCloser) error {
	o := make(map[string]string)
	if err := json.NewDecoder(rc).Decode(&o); err != nil {
		return err
	}

	// Set the state from the snapshot, no lock required according to
	// Hashicorp docs.
	s.m = o
	return nil
}

// Leading returns true when the current node is the leader.
func (s *Store) Leading() bool {
	if s == nil || s.raft == nil {
		s.logger.Error("Can't get is leader from store")
		return false
	}
	return s.raft.State() == raft.Leader
}

func (s *Store) doCompare(c Command) bool {
	switch c.Compare {
	case "":
		return true
	case "eq":
		return s.m[c.Key] == c.Versus
	}
	s.logger.WithField("cmp", c.Compare).Warn("Unrecognised comparison operator")
	return false
}

func (s *Store) applyCommand(c Command) interface{} {
	switch c.Operation {
	case "set":
		return s.applySet(c)
	case "delete":
		return s.applyDelete(c)
	case "increment":
		return s.applyIncrement(c)
	case "decrement":
		return s.applyDecrement(c)
	case "tag":
		return s.applyTag(c)
	case "untag":
		return s.applyUntag(c)
	default:
		panic(fmt.Sprintf("unrecognized command op: %s", c.Operation))
	}
}

func (s *Store) applySet(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		s.m[c.Key] = c.Value
	}
	return nil
}

func (s *Store) applyDelete(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		delete(s.m, c.Key)
	}
	return nil
}

func (s *Store) applyIncrement(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		// existing value should be an integer, if it's not it's assumed to be zero
		v, _ := strconv.Atoi(s.m[c.Key])
		inc := 1
		if c.Value != "" {
			inc, _ = strconv.Atoi(c.Value)
		}
		v += inc
		s.m[c.Key] = strconv.Itoa(v)
	}
	return nil
}

func (s *Store) applyDecrement(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		// existing value should be an integer, if it's not it's assumed to be zero
		v, _ := strconv.Atoi(s.m[c.Key])
		dec := 1
		if c.Value != "" {
			dec, _ = strconv.Atoi(c.Value)
		}
		v -= dec
		s.m[c.Key] = strconv.Itoa(v)
	}
	return nil
}

func (s *Store) applyTag(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		v := c.Value
		exist := s.m[c.Key]
		var list []string
		n := 0
		if exist != "" {
			list = strings.Split(exist, "\t")
			n = len(list)
		}

		pos := sort.SearchStrings(list, v)
		if pos < n && list[pos] == v {
			// Already exists
			return nil
		}

		if pos > n {
			list = append(list, v)
		} else if pos == 0 {
			list = append([]string{v}, list...)
		} else {
			list = append(list[:pos], append([]string{v}, list[pos:]...)...)
		}
		s.m[c.Key] = strings.Join(list, "\t")
	}
	return nil
}

func (s *Store) applyUntag(c Command) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doCompare(c) {
		v := c.Value
		exist := s.m[c.Key]
		var list []string
		n := 0
		if exist != "" {
			list = strings.Split(exist, "\t")
			n = len(list)
		}

		pos := sort.SearchStrings(list, v)
		if pos >= n || list[pos] != v {
			return nil
		}

		if pos == n-1 {
			list = list[0 : n-1]
		} else if pos == 0 {
			list = list[1:]
		} else {
			list = append(list[:pos], list[pos+1:]...)
		}
		s.m[c.Key] = strings.Join(list, "\t")

	}
	return nil
}

// Snapshot represents a complete copy of the keystore.
type Snapshot struct {
	store map[string]string
}

// Persist marshals a snapshot and writes it to the raft sink.
func (s *Snapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode data.
		b, err := json.Marshal(s.store)
		if err != nil {
			return err
		}

		// Write data to sink.
		if _, err := sink.Write(b); err != nil {
			return err
		}

		// Close the sink.
		return sink.Close()
	}()

	if err != nil {
		sink.Cancel()
	}

	return err
}

// Release causes a snapshot to be deleted.
func (s *Snapshot) Release() {}
