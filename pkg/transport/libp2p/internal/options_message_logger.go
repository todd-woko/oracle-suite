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
	"encoding/hex"

	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/transport/libp2p/crypto/ethkey"
)

// MessageLogger logs published and received messages.
func MessageLogger() Options {
	return func(n *Node) error {
		mlh := &messageLoggerHandler{n: n}
		n.AddMessageHandler(mlh)
		return nil
	}
}

type messageLoggerHandler struct {
	n *Node
}

func (m *messageLoggerHandler) Published(topic string, msg []byte) {
	if m.n.tsLog.get().Level() < log.Debug {
		return
	}
	f, mm := dumpMessage(msg)
	m.n.tsLog.get().
		WithFields(log.Fields{
			"topic":   topic,
			"message": mm,
			"format":  f,
		}).
		Debug("Published a new message")
}

func (m *messageLoggerHandler) Received(topic string, msg *pubsub.Message, _ pubsub.ValidationResult) {
	if m.n.tsLog.get().Level() < log.Debug {
		return
	}
	f, mm := dumpMessage(msg.Data)
	m.n.tsLog.get().
		WithFields(log.Fields{
			"topic":                topic,
			"message":              mm,
			"format":               f,
			"peerID":               msg.GetFrom().String(),
			"peerAddr":             ethkey.PeerIDToAddress(msg.GetFrom()).String(),
			"receivedFromPeerID":   msg.ReceivedFrom.String(),
			"receivedFromPeerAddr": ethkey.PeerIDToAddress(msg.ReceivedFrom).String(),
			"messageID":            hex.EncodeToString([]byte(msg.ID)),
		}).
		Debug("Received a new message")
}

func dumpMessage(s []byte) (string, string) {
	// TODO: Remove the text format after updating all messages to protobuf format.
	if isPrintable(s) {
		return "TEXT", string(s)
	}
	return "BINARY", hex.EncodeToString(s)
}

func isPrintable(s []byte) bool {
	for _, b := range s {
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}
