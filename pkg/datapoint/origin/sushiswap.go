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

package origin

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/defiweb/go-eth/rpc"
	"github.com/defiweb/go-eth/types"
	"golang.org/x/exp/maps"

	"github.com/chronicleprotocol/oracle-suite/pkg/datapoint"
	"github.com/chronicleprotocol/oracle-suite/pkg/datapoint/value"
	"github.com/chronicleprotocol/oracle-suite/pkg/ethereum"
	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/log/null"
	"github.com/chronicleprotocol/oracle-suite/pkg/util/bn"
)

const SushiswapLoggerTag = "SUSHISWAP_ORIGIN"

type SushiswapConfig struct {
	Client            rpc.RPC
	ContractAddresses ContractAddresses
	Logger            log.Logger
	Blocks            []int64
}

type Sushiswap struct {
	client            rpc.RPC
	contractAddresses ContractAddresses
	erc20             *ERC20
	blocks            []int64
	logger            log.Logger
}

func NewSushiswap(config SushiswapConfig) (*Sushiswap, error) {
	if config.Client == nil {
		return nil, fmt.Errorf("ethereum client not set")
	}
	if config.Logger == nil {
		config.Logger = null.New()
	}

	erc20, err := NewERC20(config.Client)
	if err != nil {
		return nil, err
	}

	return &Sushiswap{
		client:            config.Client,
		contractAddresses: config.ContractAddresses,
		erc20:             erc20,
		blocks:            config.Blocks,
		logger:            config.Logger.WithField("sushiswap", SushiswapLoggerTag),
	}, nil
}

//nolint:funlen,gocyclo
func (s *Sushiswap) FetchDataPoints(ctx context.Context, query []any) (map[any]datapoint.Point, error) {
	pairs, ok := queryToPairs(query)
	if !ok {
		return nil, fmt.Errorf("invalid query type: %T, expected []Pair", query)
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].String() < pairs[j].String()
	})

	points := make(map[any]datapoint.Point)

	block, err := s.client.BlockNumber(ctx)

	if err != nil {
		return nil, fmt.Errorf("cannot get block number, %w", err)
	}

	totals := make([]*big.Float, len(pairs))
	var calls []types.Call
	var callsToken []types.Call
	for i, pair := range pairs {
		contract, _, _, err := s.contractAddresses.ByPair(pair)
		if err != nil {
			points[pair] = datapoint.Point{Error: err}
			continue
		}

		// Calls for `getReserves`
		callData, err := getReserves.EncodeArgs()
		if err != nil {
			points[pair] = datapoint.Point{Error: fmt.Errorf(
				"failed to get reserves for pair: %s: %w",
				pair.String(),
				err,
			)}
			continue
		}
		calls = append(calls, types.Call{
			To:    &contract,
			Input: callData,
		})
		// Calls for `token0`
		callData, err = token0Abi.EncodeArgs()
		if err != nil {
			points[pair] = datapoint.Point{Error: fmt.Errorf(
				"failed to get token0 for pair: %s: %w",
				pair.String(),
				err,
			)}
			continue
		}
		callsToken = append(callsToken, types.Call{
			To:    &contract,
			Input: callData,
		})
		// Calls for `token1`
		callData, err = token1Abi.EncodeArgs()
		if err != nil {
			points[pair] = datapoint.Point{Error: fmt.Errorf(
				"failed to get token1 for pair: %s: %w",
				pair.String(),
				err,
			)}
			continue
		}
		callsToken = append(callsToken, types.Call{
			To:    &contract,
			Input: callData,
		})

		totals[i] = new(big.Float).SetInt64(0)
	}

	// Get decimals for all the tokens
	tokensMap := make(map[types.Address]struct{})
	var tokenDetails map[string]ERC20Details
	if len(callsToken) > 0 {
		resp, err := ethereum.MultiCall(ctx, s.client, callsToken, types.LatestBlockNumber)
		if err != nil {
			return nil, err
		}

		for i := range resp {
			var address types.Address
			if err := token0Abi.DecodeValues(resp[i], &address); err != nil {
				return nil, fmt.Errorf("failed decoding token address of pool: %w", err)
			}
			tokensMap[address] = struct{}{}
		}
		tokenDetails, err = s.erc20.GetSymbolAndDecimals(ctx, maps.Keys(tokensMap))
		if err != nil {
			return nil, fmt.Errorf("failed getting symbol & decimals for tokens of pool: %w", err)
		}
	}

	if len(calls) > 0 {
		for _, blockDelta := range s.blocks {
			resp, err := ethereum.MultiCall(ctx, s.client, calls, types.BlockNumberFromUint64(uint64(block.Int64()-blockDelta)))
			if err != nil {
				return nil, err
			}

			n := 0
			for i, pair := range pairs {
				if points[pair].Error != nil {
					continue
				}

				if _, ok := tokenDetails[pair.Base]; !ok {
					points[pair] = datapoint.Point{Error: fmt.Errorf("not found base token: %s", pair.Base)}
					continue
				}
				if _, ok := tokenDetails[pair.Quote]; !ok {
					points[pair] = datapoint.Point{Error: fmt.Errorf("not found quote token: %s", pair.Quote)}
					continue
				}

				baseToken := tokenDetails[pair.Base]
				quoteToken := tokenDetails[pair.Quote]

				var token0, token1 ERC20Details
				if baseToken.address.String() < quoteToken.address.String() {
					token0 = baseToken
					token1 = quoteToken
				} else {
					token0 = quoteToken
					token1 = baseToken
				}

				// token0Amount = 10 ^ token0Decimals
				token0Amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(token0.decimals)), nil)
				token1Amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(token1.decimals)), nil)

				// Reference: https://github.com/sushiswap/sushiswap-subgraph/blob/uniswap-fork/src/mappings/core.ts#L220
				var reserve0, reserve1 *big.Int
				if err := getReserves.DecodeValues(resp[n], &reserve0, &reserve1, nil); err != nil {
					points[pair] = datapoint.Point{Error: fmt.Errorf("failed decoding reserves of pool: %w",
						err)}
					continue
				}
				token0Price := big.NewFloat(0)
				if reserve1 != big.NewInt(0) {
					// token0Price = reserve0 / (10 ^ token0Decimals) / reserve1 * (10 ^ token1Decimals)
					token0Price = new(big.Float).Quo(
						new(big.Float).SetInt(new(big.Int).Mul(reserve0, token1Amount)),
						new(big.Float).SetInt(new(big.Int).Mul(token0Amount, reserve1)),
					)
				}
				token1Price := big.NewFloat(0)
				if reserve0 != big.NewInt(0) {
					// token1Price = reserve1 / (10 ^ token1Decimals) / reserve0 * (10 ^ token0Decimals)
					token1Price = new(big.Float).Quo(
						new(big.Float).SetInt(new(big.Int).Mul(reserve1, token0Amount)),
						new(big.Float).SetInt(new(big.Int).Mul(token1Amount, reserve0)),
					)
				}

				if baseToken == token0 {
					totals[i] = totals[i].Add(totals[i], token1Price)
				} else { // base token == token1
					totals[i] = totals[i].Add(totals[i], token0Price)
				}
				n++
			}
		}
	}

	for i, pair := range pairs {
		if points[pair].Error != nil {
			continue
		}
		avgPrice := new(big.Float).Quo(totals[i], new(big.Float).SetUint64(uint64(len(s.blocks))))

		tick := value.Tick{
			Pair:      pair,
			Price:     bn.Float(avgPrice),
			Volume24h: nil,
		}
		points[pair] = datapoint.Point{
			Value: tick,
			Time:  time.Now(),
		}
	}

	if len(pairs) == 1 && points[pairs[0]].Error != nil {
		return points, points[pairs[0]].Error
	}
	return points, nil
}
