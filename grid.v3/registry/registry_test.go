package registry

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"encoding/json"

	"net"

	etcdv3 "github.com/coreos/etcd/clientv3"
	"github.com/lytics/grid/grid.v3/testetcd"
)

const (
	start     = true
	dontStart = false
)

func TestInitialLeaseID(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, dontStart)
	defer etcdcleanup()
	defer client.Close()

	if r.leaseID != -1 {
		t.Fatal("lease id not initialized correctly")
	}
}

func TestStartStop(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()

	r.Stop()
	select {
	case <-r.done:
	default:
		t.Fatal("registry failed to stop")
	}
}

func TestRegister(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration")
	if err != nil {
		t.Fatal(err)
	}

	res, err := client.Get(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	reg := &Registration{}
	err = json.Unmarshal(res.Kvs[0].Value, reg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Address != r.Address() {
		t.Fatal("wrong address")
	}
	if reg.Name != r.Name() {
		t.Fatal("wrong name")
	}
}

func TestDeregistration(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	res, err := client.Get(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	reg := &Registration{}
	err = json.Unmarshal(res.Kvs[0].Value, reg)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Address != r.Address() {
		t.Fatal("wrong address")
	}
	if reg.Name != r.Name() {
		t.Fatal("wrong name")
	}

	timeout, cancel = timeoutContext()
	err = r.Deregister(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	res, err = client.Get(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 0 {
		t.Fatal("failed to deregister")
	}
}

func TestRegisterDeregisterWhileNotStarted(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, dontStart)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration")
	cancel()
	if err != ErrNotStarted {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	err = r.Deregister(timeout, "test-registration")
	cancel()
	if err != ErrNotStarted {
		t.Fatal(err)
	}
}

func TestRegisterTwiceAllowed(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	for i := 0; i < 2; i++ {
		timeout, cancel := timeoutContext()
		err := r.Register(timeout, "test-registration-twice", OpAllowReentrantRegistration)
		cancel()
		if i > 0 && err == ErrAlreadyRegistered {
			t.Fatal("not allowed to register twice")
		}
	}
}

func TestRegisterTwiceNotAllowed(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	for i := 0; i < 2; i++ {
		timeout, cancel := timeoutContext()
		err := r.Register(timeout, "test-registration-twice-b")
		cancel()
		if i > 0 && err != ErrAlreadyRegistered {
			t.Fatal("allowed to register twice")
		}
	}
}

func TestStop(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration")
	if err != nil {
		t.Fatal(err)
	}

	r.Stop()
	res, err := client.Get(timeout, "test-registration")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 0 {
		t.Fatal("stopping heartbeat did not delete keys")
	}
}

func TestFindRegistration(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration-a")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	err = r.Register(timeout, "test-registration-aa")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	reg, err := r.FindRegistration(timeout, "test-registration-a")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if reg.Key != "test-registration-a" {
		t.Fatal("got wrong key")
	}
}

func TestFindRegistrations(t *testing.T) {
	client, r, _, etcdcleanup := bootstrap(t, start)
	defer etcdcleanup()
	defer client.Close()
	defer r.Stop()

	timeout, cancel := timeoutContext()
	err := r.Register(timeout, "test-registration-a")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	err = r.Register(timeout, "test-registration-aa")
	cancel()
	if err != nil {
		t.Fatal(err)
	}

	timeout, cancel = timeoutContext()
	regs, err := r.FindRegistrations(timeout, "test-registration-a")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if len(regs) != 2 {
		t.Fatal("failed to find number of expected registrations")
	}

	expected := map[string]bool{
		"test-registration-a":  true,
		"test-registration-aa": true,
	}
	for _, reg := range regs {
		delete(expected, reg.Key)
	}
	if len(expected) != 0 {
		t.Fatal("failed to find all expected registrations")
	}
}

func TestKeepAlive(t *testing.T) {
	client, r, addr, etcdcleanup := bootstrap(t, dontStart)
	defer etcdcleanup()
	defer client.Close()

	// Change the minimum for sake of testing quickly.
	minLeaseDuration = 1 * time.Second

	// Use the minimum.
	r.LeaseDuration = 1 * time.Second
	_, err := r.Start(addr)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the number of heartbeats matching some reasonable
	// expected minimum. Keep in mind that each lease duration
	// should produce "hearbratsPerLeaseDuration" heartbeats.
	time.Sleep(5 * time.Second)
	r.Stop()
	if float64(r.keepAliveStats.success) < float64(heartbeatsPerLeaseDuration)*5*0.9 {
		t.Fatal("too few keep alive heartbeats")
	}
}

func TestWatch(t *testing.T) {
	client, r, addr, etcdcleanup := bootstrap(t, dontStart)
	defer etcdcleanup()
	defer client.Close()

	// Change the minimum for sake of testing quickly.
	minLeaseDuration = 1 * time.Second

	// Use the minimum.
	r.LeaseDuration = 1 * time.Second
	_, err := r.Start(addr)
	if err != nil {
		t.Fatal(err)
	}

	initial := map[string]bool{
		"peer-1": true,
		"peer-2": true,
		"peer-3": true,
	}

	for peer := range initial {
		timeout, cancel := timeoutContext()
		err := r.Register(timeout, peer)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}

	watching := make(chan error)
	ctx, cancelWatch := context.WithCancel(context.Background())
	go func() {
		found, _, err := r.Watch(ctx, "peer")
		if err != nil {
			watching <- err
			return
		}
		for _, peer := range found {
			if !initial[peer.Key] {
				watching <- fmt.Errorf("missing initial peer: " + peer.Key)
				return
			}
		}
		close(watching)
	}()

	err = <-watching
	if err != nil {
		t.Fatal(err)
	}

	additions := map[string]bool{
		"peer-4": true,
		"peer-5": true,
	}

	for peer := range additions {
		timeout, cancel := timeoutContext()
		err := r.Register(timeout, peer)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
	}

	for peer := range initial {
		timeout, cancel := timeoutContext()
		err := r.Deregister(timeout, peer)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Second)
	}

	cancelWatch()
	time.Sleep(3 * time.Second)
	r.Stop()
	etcdcleanup()
}

func bootstrap(t *testing.T, shouldStart bool) (*etcdv3.Client, *Registry, *net.TCPAddr, testetcd.Cleanup) {
	client, cleanup := testetcd.StartAndConnect(t)

	addr := &net.TCPAddr{
		IP:   []byte("localhost"),
		Port: 2000 + rand.Intn(2000),
	}

	r, err := New(client)
	if err != nil {
		t.Fatal(err)
	}
	r.LeaseDuration = 10 * time.Second

	if shouldStart {
		_, err = r.Start(addr)
		if err != nil {
			t.Fatal(err)
		}
	}

	return client, r, addr, cleanup
}

func timeoutContext() (context.Context, func()) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}
