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

package messages

import (
	"math/big"

	"github.com/defiweb/go-eth/types"

	"github.com/chronicleprotocol/oracle-suite/pkg/transport/messages/pb"

	"google.golang.org/protobuf/proto"
)

const GreetV1MessageName = "greet/v1"

type Greet struct {
	Signature  types.Signature `json:"signature"`
	PublicKeyX *big.Int        `json:"publicKeyX"`
	PublicKeyY *big.Int        `json:"publicKeyY"`
}

// MarshallBinary implements the transport.Message interface.
func (e *Greet) MarshallBinary() ([]byte, error) {
	return proto.Marshal(&pb.Greet{
		Signature: e.Signature.Bytes(),
		PubKeyX:   e.PublicKeyX.Bytes(),
		PubKeyY:   e.PublicKeyY.Bytes(),
	})
}

// UnmarshallBinary implements the transport.Message interface.
func (e *Greet) UnmarshallBinary(data []byte) (err error) {
	msg := pb.Greet{}
	if err := proto.Unmarshal(data, &msg); err != nil {
		return err
	}
	e.Signature, err = types.SignatureFromBytes(msg.Signature)
	if err != nil {
		return err
	}
	e.PublicKeyX = new(big.Int).SetBytes(msg.PubKeyX)
	e.PublicKeyY = new(big.Int).SetBytes(msg.PubKeyY)
	return nil
}
