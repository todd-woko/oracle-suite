//  Copyright (C) 2021-2023 Chronicle Labs, Inc.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package internal

import (
	"github.com/defiweb/go-eth/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	"github.com/chronicleprotocol/oracle-suite/pkg/transport/libp2p/crypto/ethkey"
	"github.com/chronicleprotocol/oracle-suite/pkg/transport/libp2p/internal/sets"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
)

type PeerInfo struct {
	peerID          peer.ID
	peerAddr        types.Address
	topic           string
	listens         []multiaddr.Multiaddr
	userAgent       string
	protocolVersion string
	protocols       []protocol.ID
}

func PeerInfos(c chan<- PeerInfo) Options {
	return func(n *Node) error {
		n.AddPubSubEventHandler(sets.PubSubEventHandlerFunc(func(topic string, event pubsub.PeerEvent) {
			p := n.Peerstore()
			pp, _ := p.GetProtocols(event.Peer)
			c <- PeerInfo{
				peerID:          event.Peer,
				peerAddr:        ethkey.PeerIDToAddress(event.Peer),
				topic:           topic,
				listens:         p.PeerInfo(event.Peer).Addrs,
				userAgent:       getPeerUserAgent(p, event.Peer),
				protocolVersion: getPeerProtocolVersion(p, event.Peer),
				protocols:       pp,
			}
		}))
		return nil
	}
}

// PeerLogger logs all peers handled by libp2p's pubsub system.
func PeerLogger() Options {
	return func(n *Node) error {
		n.AddPubSubEventHandler(sets.PubSubEventHandlerFunc(func(topic string, event pubsub.PeerEvent) {
			p := n.Peerstore()

			ad := p.PeerInfo(event.Peer).Addrs
			ua := getPeerUserAgent(p, event.Peer)
			pp := getPeerProtocols(p, event.Peer)
			pv := getPeerProtocolVersion(p, event.Peer)
			pa := ethkey.PeerIDToAddress(event.Peer)

			switch event.Type {
			case pubsub.PeerJoin:
				n.tsLog.get().
					WithFields(log.Fields{
						"peerID":          event.Peer,
						"peerAddr":        pa,
						"topic":           topic,
						"listenAddrs":     ad,
						"userAgent":       ua,
						"protocolVersion": pv,
						"protocols":       pp,
					}).
					Debug("Connected to a peer")
			case pubsub.PeerLeave:
				n.tsLog.get().
					WithFields(log.Fields{
						"peerID":   event.Peer,
						"peerAddr": pa,
						"topic":    topic,
					}).
					Debug("Disconnected from a peer")
			}
		}))
		return nil
	}
}

func getPeerProtocols(ps peerstore.Peerstore, pid peer.ID) []string {
	pp, _ := ps.GetProtocols(pid)
	return protocol.ConvertToStrings(pp)
}

func getPeerUserAgent(ps peerstore.Peerstore, pid peer.ID) string {
	av, _ := ps.Get(pid, "AgentVersion")
	if s, ok := av.(string); ok {
		return s
	}
	return ""
}

func getPeerProtocolVersion(ps peerstore.Peerstore, pid peer.ID) string {
	av, _ := ps.Get(pid, "ProtocolVersion")
	if s, ok := av.(string); ok {
		return s
	}
	return ""
}
