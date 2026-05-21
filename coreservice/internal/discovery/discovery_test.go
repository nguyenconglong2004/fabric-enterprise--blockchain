package discovery

import (
	"testing"

	"coreservice/internal/network"
)

func TestLeaderIsAlive(t *testing.T) {
	mv := &network.MembershipView{
		LeaderID: "12D3KooWLeader",
		Members: []network.MemberInfo{
			{ID: "12D3KooWLeader", Alive: false, Addresses: []string{"/ip4/127.0.0.1/tcp/6000"}},
			{ID: "12D3KooWFollower", Alive: true, Addresses: []string{"/ip4/127.0.0.1/tcp/6001/p2p/12D3KooWFollower"}},
		},
	}
	if LeaderIsAlive(mv) {
		t.Fatal("expected dead leader")
	}
	mv.Members[0].Alive = true
	if !LeaderIsAlive(mv) {
		t.Fatal("expected alive leader")
	}
}

func TestPickOrdererAddr_prefersAliveLeader(t *testing.T) {
	mv := &network.MembershipView{
		LeaderID: "12D3KooWLeader",
		Members: []network.MemberInfo{
			{ID: "12D3KooWLeader", Alive: true, Addresses: []string{"/ip4/127.0.0.1/tcp/6000/p2p/12D3KooWLeader"}},
			{ID: "12D3KooWF1", Alive: true, Addresses: []string{"/ip4/127.0.0.1/tcp/6001/p2p/12D3KooWF1"}},
		},
	}
	addr, err := PickOrdererAddr(mv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "/ip4/127.0.0.1/tcp/6000/p2p/12D3KooWLeader" {
		t.Fatalf("got %q", addr)
	}
}

func TestPickOrdererAddr_staleLeaderFallsBack(t *testing.T) {
	mv := &network.MembershipView{
		LeaderID: "12D3KooWDead",
		Members: []network.MemberInfo{
			{ID: "12D3KooWDead", Alive: false, Addresses: []string{"/ip4/127.0.0.1/tcp/6000"}},
			{ID: "12D3KooWF1", Alive: true, Addresses: []string{"/ip4/127.0.0.1/tcp/6001/p2p/12D3KooWF1"}},
		},
	}
	normalizeLeader(mv)
	addr, err := PickOrdererAddr(mv)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "/ip4/127.0.0.1/tcp/6001/p2p/12D3KooWF1" {
		t.Fatalf("got %q", addr)
	}
}

func TestParseBootstraps(t *testing.T) {
	got := ParseBootstraps("/ip4/1/tcp/6000/p2p/A", "/ip4/2/tcp/6001/p2p/B, /ip4/1/tcp/6000/p2p/A")
	if len(got) != 2 {
		t.Fatalf("want 2 unique, got %d: %v", len(got), got)
	}
}
