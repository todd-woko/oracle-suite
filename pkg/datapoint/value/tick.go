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

package value

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/chronicleprotocol/oracle-suite/pkg/datapoint/origin/pb"
	"github.com/chronicleprotocol/oracle-suite/pkg/util/bn"
)

// TickPricePrecision specified number of decimal places for tick prices
// during marshaling.
const TickPricePrecision = 18

// TickVolumePrecision specified number of decimal places for tick volumes
// during marshaling.
const TickVolumePrecision = 18

// Tick contains a price, volume and other information for a given asset pair
// at a given time.
//
// Before using this data, you should check if it is valid by calling
// Tick.Validate() method.
//
// During marshaling, the price and volume are converted to fixed-point
// numbers with the precision specified by TickPricePrecision and
// TickVolumePrecision constants.
type Tick struct {
	// Pair is an asset pair for which this price is calculated.
	Pair Pair

	// Price is a price for the given asset pair.
	// Depending on the provider implementation, this price can be
	// a last trade price, an average of bid and ask prices, etc.
	//
	// Price is always non-nil if there is no error.
	Price *bn.FloatNumber

	// Volume24h is a 24h volume for the given asset pair presented in the
	// base currency.
	//
	// May be nil if the provider does not provide volume.
	Volume24h *bn.FloatNumber
}

// Number implements the NumericValue interface.
func (t Tick) Number() *bn.FloatNumber {
	return t.Price
}

// Print implements the Value interface.
func (t Tick) Print() string {
	return fmt.Sprintf("Pair=%s, Price=%s, Volume24h=%s", t.Pair, t.Price, t.Volume24h)
}

// MarshalBinary implements the Value interface.
func (t Tick) MarshalBinary() ([]byte, error) {
	var (
		priceBytes     []byte
		volume24HBytes []byte
		err            error
	)
	if t.Price != nil {
		priceBytes, err = t.Price.DecFixedPoint(TickPricePrecision).MarshalBinary()
		if err != nil {
			return nil, err
		}
	}
	if t.Volume24h != nil {
		volume24HBytes, err = t.Volume24h.DecFixedPoint(TickVolumePrecision).MarshalBinary()
		if err != nil {
			return nil, err
		}
	}
	return proto.Marshal(&pb.Tick{
		Pair:      t.Pair.String(),
		Price:     priceBytes,
		Volume24H: volume24HBytes,
	})
}

// UnmarshalBinary implements the Value interface.
func (t *Tick) UnmarshalBinary(bytes []byte) error {
	pbTick := &pb.Tick{}
	if err := proto.Unmarshal(bytes, pbTick); err != nil {
		return err
	}
	pair, err := PairFromString(pbTick.Pair)
	if err != nil {
		return err
	}
	t.Pair = pair
	if len(pbTick.Price) > 0 {
		price := &bn.DecFixedPointNumber{}
		if err := price.UnmarshalBinary(pbTick.Price); err != nil {
			return err
		}
		t.Price = price.Float()
	}
	if len(pbTick.Volume24H) > 0 {
		volume24h := &bn.DecFixedPointNumber{}
		if err := volume24h.UnmarshalBinary(pbTick.Volume24H); err != nil {
			return err
		}
		t.Volume24h = volume24h.Float()
	}
	return nil
}

// Validate returns an error if the tick is invalid.
func (t Tick) Validate() error {
	if t.Pair.Empty() {
		return fmt.Errorf("pair is not set")
	}
	if t.Price == nil {
		return fmt.Errorf("price is nil")
	}
	if t.Price.Sign() <= 0 {
		return fmt.Errorf("price is zero or negative")
	}
	if t.Price.IsInf() {
		return fmt.Errorf("price is infinite")
	}
	if t.Volume24h != nil && t.Volume24h.Sign() < 0 {
		return fmt.Errorf("volume is negative")
	}
	return nil
}

func (t Tick) MarshalJSON() ([]byte, error) {
	var volume24h string
	if t.Volume24h != nil {
		volume24h = t.Volume24h.String()
	}
	return json.Marshal(map[string]interface{}{
		"pair":      t.Pair.String(),
		"price":     t.Price.String(),
		"volume24h": volume24h,
	})
}

func (t *Tick) UnmarshalJSON(data []byte) error {
	result := make(map[string]interface{})
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	pair, err := PairFromString(result["pair"].(string))
	if err != nil {
		return err
	}
	t.Pair = pair
	t.Price = bn.Float(result["price"].(string))
	t.Volume24h = bn.Float(result["volume24h"].(string))
	return nil
}

// Pair represents an asset pair.
type Pair struct {
	Base  string
	Quote string
}

// PairFromString returns a new Pair for given string.
// The string must be formatted as "BASE/QUOTE".
func PairFromString(s string) (p Pair, err error) {
	return p, p.UnmarshalText([]byte(s))
}

// MarshalText implements encoding.TextMarshaler interface.
func (p Pair) MarshalText() ([]byte, error) {
	return []byte(p.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler interface.
func (p *Pair) UnmarshalText(text []byte) error {
	ss := strings.Split(string(text), "/")
	if len(ss) != 2 {
		return fmt.Errorf("pair must be formatted as BASE/QUOTE, got %q", string(text))
	}
	p.Base = strings.ToUpper(ss[0])
	p.Quote = strings.ToUpper(ss[1])
	return nil
}

// Empty returns true if the pair is empty.
// Pair is considered empty if either base or quote is empty.
func (p Pair) Empty() bool {
	return p.Base == "" || p.Quote == ""
}

// Equal returns true if the pair is equal to the given pair.
func (p Pair) Equal(c Pair) bool {
	return p.Base == c.Base && p.Quote == c.Quote
}

// Invert returns an inverted pair.
// For example, if the pair is "BTC/USD", then the inverted pair is "USD/BTC".
func (p Pair) Invert() Pair {
	return Pair{
		Base:  p.Quote,
		Quote: p.Base,
	}
}

// String returns a string representation of the pair.
func (p Pair) String() string {
	return fmt.Sprintf("%s/%s", p.Base, p.Quote)
}
