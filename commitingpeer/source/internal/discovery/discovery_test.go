package discovery

import (
	"testing"

	"commiting-peer/internal/deliver"
)

func TestPickOrdererAddr_staleLeader(t *testing.T) {
	mv := &deliver.MembershipView{
		LeaderID: "dead",
		Members: []deliver.MemberInfo{
			{ID: "dead", Alive: false, Priority: 0, Addresses: []string{"/ip4/127.0.0.1/tcp/6000"}},
			{ID: "f1", Alive: true, Priority: 1, Addresses: []string{"/ip4/127.0.0.1/tcp/6001/p2p/f1"}},
		},
	}
	normalizeLeader(mv)
	addr, err := PickOrdererAddr(mv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "/ip4/127.0.0.1/tcp/6001/p2p/f1" {
		t.Fatalf("got %q", addr)
	}
}
