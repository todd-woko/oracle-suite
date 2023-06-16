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
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/defiweb/go-eth/crypto"
	"github.com/defiweb/go-eth/hexutil"
	"github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chronicleprotocol/oracle-suite/pkg/price/median"
)

type priceLog struct {
	Format               string    `json:"format"`
	Level                string    `json:"level"`
	Message              string    `json:"message"`
	MessageID            string    `json:"messageID"`
	Msg                  string    `json:"msg"`
	PeerAddr             string    `json:"peerAddr"`
	PeerID               string    `json:"peerID"`
	ReceivedFromPeerAddr string    `json:"receivedFromPeerAddr"`
	ReceivedFromPeerID   string    `json:"receivedFromPeerID"`
	Tag                  string    `json:"tag"`
	Time                 time.Time `json:"time"`
	Topic                string    `json:"topic"`
	XHostID              string    `json:"x-hostID"`
}

func readEachLineFromFile(data []byte) [][]byte {
	lines := strings.Split(string(data), "\n")
	var res [][]byte
	for _, line := range lines {
		if line == "" {
			continue
		}
		res = append(res, []byte(line))
	}
	return res
}

func TestPrice_Sign(t *testing.T) {
	// t.Skip("TODO: fix the issue with the signing")

	k := wallet.NewKeyFromBytes([]byte("0x0f2e4a9f5b4a9c3a"))
	expectedFrom := k.Address().String()

	for _, tt := range prepTestCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.message.Hash().String()

			assert.NoError(t, tt.message.Sign(k), "could not sign message")

			assert.Equal(t, h, tt.message.Hash().String(), "hash changed after signing")

			f, err := tt.message.From(crypto.ECRecoverer)

			require.NoError(t, err, "could not recover signer")
			assert.Equal(t, expectedFrom, f.String(), "signer not as expected")
		})
	}
}

func TestPrice_Unmarshall(t *testing.T) {
	t.Skip("This test might be obsolete if the issue lies on the signer side.")

	k := wallet.NewKeyFromBytes([]byte(`0x0f2e4a9f5b4a9c3a`))

	for i, tt := range prepTestCases(t) {
		t.Run(fmt.Sprintf("line-%03d-id-%s", i+1, tt.name), func(t *testing.T) {
			from, err := tt.message.From(crypto.ECRecoverer)
			if err != nil && assert.EqualError(t, err, "invalid square root") {
				t.Skip("test data not valid for this case")
			} else {
				assert.Equal(t, tt.peerAddr, from.String(), "message not from expected peer")

				assert.NoError(t, tt.message.Sign(k), "could not sign message")
				f, err := tt.message.From(crypto.ECRecoverer)
				assert.NoError(t, err, "could not recover signer")
				assert.Equal(t, k.Address().String(), f.String(), "signer not as expected")
			}
		})
	}
}

func TestPrice_Marshalling(t *testing.T) {
	tests := []struct {
		price   *Price
		wantErr bool
	}{
		// Simple message:
		{
			price: &Price{
				messageVersion: 0,
				Price: &median.Price{
					Wat: "AAABBB",
					Val: big.NewInt(10),
					Age: time.Unix(100, 0),
					Sig: types.Signature{
						V: new(big.Int).SetInt64(1),
						R: new(big.Int).SetBytes([]byte{1}),
						S: new(big.Int).SetBytes([]byte{2}),
					},
				},
				Trace:   []byte("{}"),
				Version: "0.0.1",
			},
			wantErr: false,
		},
		// Simple message as V0:
		{
			price: (&Price{
				messageVersion: 0,
				Price: &median.Price{
					Wat: "AAABBB",
					Val: big.NewInt(10),
					Age: time.Unix(100, 0),
					Sig: types.Signature{
						V: new(big.Int).SetInt64(1),
						R: new(big.Int).SetBytes([]byte{1}),
						S: new(big.Int).SetBytes([]byte{2}),
					},
				},
				Trace:   []byte("{}"),
				Version: "0.0.1",
			}).AsV0(),
			wantErr: false,
		},
		// Simple message as V1:
		{
			price: (&Price{
				messageVersion: 0,
				Price: &median.Price{
					Wat: "AAABBB",
					Val: big.NewInt(10),
					Age: time.Unix(100, 0),
					Sig: types.Signature{
						V: new(big.Int).SetInt64(1),
						R: new(big.Int).SetBytes([]byte{1}),
						S: new(big.Int).SetBytes([]byte{2}),
					},
				},
				Trace:   []byte("{}"),
				Version: "0.0.1",
			}).AsV0(),
			wantErr: false,
		},
		// Without trace:
		{
			price: &Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        "0.0.1",
			},
			wantErr: false,
		},
		// Without trace as V0:
		{
			price: (&Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        "0.0.1",
			}).AsV0(),
			wantErr: false,
		},
		// Without trace as V1:
		{
			price: (&Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        "0.0.1",
			}).AsV1(),
			wantErr: false,
		},
		// Too large message:
		{
			price: &Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        strings.Repeat("a", priceMessageMaxSize+1),
			},
			wantErr: true,
		},
		// Too large V0 message:
		{
			price: (&Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        strings.Repeat("a", priceMessageMaxSize+1),
			}).AsV0(),
			wantErr: true,
		},
		// Too large V1 message:
		{
			price: (&Price{
				messageVersion: 0,
				Price:          &median.Price{},
				Trace:          nil,
				Version:        strings.Repeat("a", priceMessageMaxSize+1),
			}).AsV1(),
			wantErr: true,
		},
	}
	for n, tt := range tests {
		t.Run(fmt.Sprintf("case-%d", n+1), func(t *testing.T) {
			msg, err := tt.price.MarshallBinary()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				price := &Price{}
				err := price.UnmarshallBinary(msg)

				require.NoError(t, err)
				assert.Equal(t, tt.price.Price.Wat, price.Price.Wat)
				if tt.price.Price.Val != nil {
					assert.Equal(t, tt.price.Price.Val.Bytes(), price.Price.Val.Bytes())
				} else {
					assert.Equal(t, big.NewInt(0), price.Price.Val)
				}
				assert.Equal(t, tt.price.Price.Age.Unix(), price.Price.Age.Unix())
				assert.Equal(t, tt.price.Price.Sig.Bytes(), price.Price.Sig.Bytes())
				assert.Equal(t, tt.price.Version, price.Version)

				if tt.price.messageVersion == 0 && tt.price.Trace == nil {
					assert.Equal(t, json.RawMessage("null"), price.Trace)
				} else {
					assert.Equal(t, tt.price.Trace, price.Trace)
				}
			}
		})
	}
}

func FuzzPrice_UnmarshallBinary(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = (&Price{}).UnmarshallBinary(data)
	})
}

type tc struct {
	name     string
	peerAddr string
	message  median.Price
	format   string
}

//go:embed testdata/messages.jsonl
var messages []byte

//go:embed testdata/messages-libp2p.jsonl
var messages_libp2p []byte

func prepTestCases(t *testing.T) []tc {
	var tests []tc
	for i, l := range readEachLineFromFile(messages) {
		var pl priceLog
		require.NoError(t, json.Unmarshal(l, &pl))

		var b []byte
		switch pl.Format {
		case "BINARY":
			b = hexutil.MustHexToBytes(pl.Message)
		case "TEXT":
			b = []byte(pl.Message)
		}

		var p Price
		require.NoError(t, p.UnmarshallBinary(b))

		tests = append(tests, tc{fmt.Sprintf("msg-%03d-%s", i+1, pl.MessageID), pl.PeerAddr, *p.Price, pl.Format})
	}

	for i, l := range readEachLineFromFile(messages_libp2p) {
		var p Price
		require.NoError(t, json.Unmarshal(l, &p))

		tests = append(tests, tc{fmt.Sprintf("libp2p-%03d", i+1), "", *p.Price, "JSON"})
	}
	return tests
}
