package cluster

import (
	"fmt"
	"testing"
	"time"

	"context"
	"errors"
	"sort"

	"reflect"

	"github.com/go-kit/kit/log"
	"github.com/improbable-eng/thanos/pkg/runutil"
	"github.com/improbable-eng/thanos/pkg/store/storepb"
	"github.com/improbable-eng/thanos/pkg/testutil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/timestamp"
)

func joinPeer(num int, knownPeers []string) (peerAddr string, peer *Peer, err error) {
	port, err := testutil.FreePort()
	if err != nil {
		return "", nil, err
	}
	peerAddr = fmt.Sprintf("127.0.0.1:%d", port)
	now := time.Now()
	peerState1 := PeerState{
		Type:    PeerTypeSource,
		APIAddr: fmt.Sprintf("sidecar-address:%d", num),
		Metadata: PeerMetadata{
			Labels: []storepb.Label{
				{
					Name:  "a",
					Value: fmt.Sprintf("%d", num),
				},
			},
			MinTime: timestamp.FromTime(now.Add(-10 * time.Minute)),
			MaxTime: timestamp.FromTime(now.Add(-1 * time.Second)),
		},
	}

	peer, err = Join(
		log.NewNopLogger(),
		prometheus.NewRegistry(),
		peerAddr,
		peerAddr,
		knownPeers,
		peerState1,
		false,
		100*time.Millisecond,
		50*time.Millisecond,
	)

	return peerAddr, peer, nil
}

func sortStr(str []string) []string {
	sort.Strings(str)
	return str
}

func TestPeers_PropagatingState(t *testing.T) {
	addr1, peer1, err := joinPeer(1, nil)
	testutil.Ok(t, err)

	addr2, peer2, err := joinPeer(2, []string{addr1})
	testutil.Ok(t, err)

	// peer2 should see two members with their data.
	expected := sortStr([]string{addr1, addr2})
	testutil.Equals(t, expected, sortStr(peer2.Peers(PeerTypeSource)))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	testutil.Ok(t, runutil.Retry(1*time.Second, ctx.Done(), func() error {
		if len(peer1.data) > 1 {
			return nil
		}
		return errors.New("I am alone here")
	}))

	// peer1 should see two members with their data.
	testutil.Equals(t, expected, sortStr(peer1.Peers(PeerTypeSource)))

	// Update peer1 state.
	now := time.Now()
	newPeerMeta1 := PeerMetadata{
		Labels: []storepb.Label{
			{
				Name:  "b",
				Value: "1",
			},
		},
		MinTime: timestamp.FromTime(now.Add(-20 * time.Minute)),
		MaxTime: timestamp.FromTime(now.Add(-1 * time.Millisecond)),
	}
	peer1.SetLabels(newPeerMeta1.Labels)
	peer1.SetTimestamps(newPeerMeta1.MinTime, newPeerMeta1.MaxTime)

	// Check if peer2 got the updated meta about peer1.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	testutil.Ok(t, runutil.Retry(1*time.Second, ctx2.Done(), func() error {
		for _, st := range peer2.PeerStates(PeerTypeSource) {
			if st.APIAddr != "sidecar-address:1" {
				continue
			}

			if reflect.DeepEqual(st.Metadata, newPeerMeta1) {
				return nil
			}
		}
		return errors.New("outdated metadata")
	}))
}
