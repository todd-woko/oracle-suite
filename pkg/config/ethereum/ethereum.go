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

package ethereum

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/defiweb/go-eth/rpc"
	"github.com/defiweb/go-eth/rpc/transport"
	"github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	"github.com/hashicorp/hcl/v2"

	"github.com/chronicleprotocol/oracle-suite/pkg/config"
	"github.com/chronicleprotocol/oracle-suite/pkg/log"
	"github.com/chronicleprotocol/oracle-suite/pkg/rpcsplitter"
)

const (
	splitterVirtualHost    = "rpc-splitter"
	defaultTotalTimeout    = 10
	defaultGracefulTimeout = 1
)

type (
	KeyRegistry    map[string]wallet.Key
	ClientRegistry map[string]rpc.RPC
)

type Dependencies struct {
	// Logger is the logger that is used by RPC-Splitter.
	Logger log.Logger
}

// Config contains the configuration for Ethereum clients and keys.
type Config struct {
	// Keys is a list of Ethereum keys.
	Keys []ConfigKey `hcl:"key,block"`

	// RandKeys is a list of random keys.
	RandKeys []string `hcl:"rand_keys,optional"`

	// Clients is a list of Ethereum clients.
	Clients []ConfigClient `hcl:"client,block"`

	// HCL fields:
	Content hcl.BodyContent `hcl:",content"`

	// Configured services:
	prepared bool
	keys     KeyRegistry
	clients  ClientRegistry
}

// ConfigKey contains the configuration for an Ethereum key.
type ConfigKey struct {
	// Name is the unique name of the key that can be referenced by other
	// services.
	Name string `hcl:"name,label"`

	// Address is the address of the key in hex format.
	Address types.Address `hcl:"address"`

	// KeystorePath is the path to the keystore directory.
	KeystorePath string `hcl:"keystore_path"`

	// PassphraseFile is the path to the file containing the passphrase for the
	// key. If empty, then the passphrase is not provided.
	PassphraseFile string `hcl:"passphrase_file,optional"`

	// HCL fields:
	Content hcl.BodyContent `hcl:",content"`

	// Configured key:
	key wallet.Key
}

// ConfigClient contains the configuration for an Ethereum client.
type ConfigClient struct {
	// Name is the unique name of the client that can be referenced by other
	// services.
	Name string `hcl:"name,label"`

	// RPCURLs is a list of RPC URLs to use for the client. If multiple URLs
	// are provided, then RPC-Splitter will be used.
	RPCURLs []config.URL `hcl:"rpc_urls"`

	// RPC-Splitter timeout settings:

	// Total timeout for the request, in seconds.
	Timeout uint32 `hcl:"timeout,optional"`

	// GracefulTimeout is the time to wait for the response, in seconds, for
	// slower nodes after reaching minimum number of responses required for the
	// request.
	GracefulTimeout uint32 `hcl:"graceful_timeout,optional"`

	// MaxBlocksBehind is the maximum number of blocks behind the node with the
	// highest block number can be. RPC-Splitter will use the lowest block number
	// from all nodes that is not more than MaxBlocksBehind behind the highest
	// block number.
	MaxBlocksBehind uint64 `hcl:"max_blocks_behind,optional"`

	// Key configuration:

	// EthereumKey is the name of the Ethereum key to use for signing
	// transactions.
	EthereumKey string `hcl:"ethereum_key,optional"`

	// ChainID is the chain ID to use for signing transactions.
	ChainID uint64 `hcl:"chain_id,optional"`

	// HCL fields:
	Range   hcl.Range       `hcl:",range"`
	Content hcl.BodyContent `hcl:",content"`

	// Configured services:
	client rpc.RPC
}

// KeyRegistry returns the list of configured Ethereum keys.
func (c *Config) KeyRegistry(d Dependencies) (KeyRegistry, error) {
	if c == nil {
		return nil, nil
	}
	if err := c.prepare(d); err != nil {
		return nil, err
	}
	return c.keys, nil
}

// ClientRegistry returns the list of configured Ethereum clients.
func (c *Config) ClientRegistry(d Dependencies) (ClientRegistry, error) {
	if c == nil {
		return nil, nil
	}
	if err := c.prepare(d); err != nil {
		return nil, err
	}
	return c.clients, nil
}

func (c *Config) prepare(d Dependencies) error {
	if c.prepared {
		return nil
	}
	if err := c.prepareKeys(); err != nil {
		return err
	}
	if err := c.prepareClients(d.Logger); err != nil {
		return err
	}
	c.prepared = true
	return nil
}

func (c *Config) prepareKeys() error {
	// Keys from the keystore.
	c.keys = make(map[string]wallet.Key)
	for _, keyCfg := range c.Keys {
		key, err := keyCfg.Key()
		if err != nil {
			return err
		}
		c.keys[keyCfg.Name] = key
	}

	// Random keys.
	for _, name := range c.RandKeys {
		if len(name) == 0 {
			return &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Validation error",
				Detail:   "Random key name must not be empty",
				Subject:  c.Content.Attributes["rand_keys"].Range.Ptr(),
			}
		}
		if !nameRegexp.MatchString(name) {
			return &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Validation error",
				Detail:   "Random key name must contain only alphanumeric characters and underscores",
				Subject:  c.Content.Attributes["rand_keys"].Range.Ptr(),
			}
		}
		if _, ok := c.keys[name]; ok {
			return &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Validation error",
				Detail:   fmt.Sprintf("Key with name %q already exists", name),
				Subject:  c.Content.Attributes["rand_keys"].Range.Ptr(),
			}
		}
		c.keys[name] = wallet.NewRandomKey()
	}
	return nil
}

func (c *Config) prepareClients(logger log.Logger) error {
	c.clients = make(map[string]rpc.RPC)
	for _, clientCfg := range c.Clients {
		if _, ok := c.clients[clientCfg.Name]; ok {
			return &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Validation error",
				Detail:   fmt.Sprintf("Client with name %q already exists", clientCfg.Name),
				Subject:  clientCfg.Range.Ptr(),
			}
		}
		client, err := clientCfg.Client(logger, c.keys)
		if err != nil {
			return err
		}
		c.clients[clientCfg.Name] = client
	}
	return nil
}

// Key returns the configured Ethereum key.
func (c *ConfigKey) Key() (wallet.Key, error) {
	if c == nil {
		return nil, fmt.Errorf("ethereum config: key is not configured")
	}
	if c.key != nil {
		return c.key, nil
	}

	// Validate the key configuration.
	if len(c.Name) == 0 {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Ethereum key name is required",
			Subject:  c.Content.Attributes["name"].Range.Ptr(),
		}
	}
	if !nameRegexp.MatchString(c.Name) {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Ethereum key name must contain only alphanumeric characters and underscores",
			Subject:  c.Content.Attributes["name"].Range.Ptr(),
		}
	}

	// Create key.
	passphrase, err := readAccountPassphrase(c.PassphraseFile)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   fmt.Sprintf("Failed to read Ethereum key passphrase: %v", err),
			Subject:  c.Content.Attributes["passphrase_file"].Range.Ptr(),
		}
	}
	key, err := wallet.NewKeyFromDirectory(c.KeystorePath, passphrase, c.Address)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   fmt.Sprintf("Failed to read Ethereum key: %v", err),
			Subject:  c.Content.Attributes["keystore_path"].Range.Ptr(),
		}
	}

	c.key = key
	return key, nil
}

const LoggerTag = "CONFIG_ETHEREUM"

// Client returns the configured RPC client.
func (c *ConfigClient) Client(logger log.Logger, keys KeyRegistry) (rpc.RPC, error) {
	if c == nil {
		return nil, fmt.Errorf("ethereum config: client is not configured")
	}
	if c.client != nil {
		return c.client, nil
	}

	logger = logger.
		WithField("tag", LoggerTag)

	// Validate the client configuration.
	if len(c.Name) == 0 {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Ethereum client name is required",
			Subject:  c.Content.Attributes["name"].Range.Ptr(),
		}
	}
	if !nameRegexp.MatchString(c.Name) {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Ethereum client name must contain only alphanumeric characters and underscores",
			Subject:  c.Content.Attributes["name"].Range.Ptr(),
		}
	}
	if len(c.RPCURLs) == 0 {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "At least one RPC URL is required",
			Subject:  c.Content.Attributes["rpc_urls"].Range.Ptr(),
		}
	}
	if c.Timeout == 0 {
		c.Timeout = defaultTotalTimeout
	}
	if c.GracefulTimeout == 0 {
		c.GracefulTimeout = defaultGracefulTimeout
	}
	if c.Timeout < 1 {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Timeout cannot be less than one second",
			Subject:  c.Content.Attributes["timeout"].Range.Ptr(),
		}
	}
	if c.GracefulTimeout < 1 {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Validation error",
			Detail:   "Graceful timeout cannot be less than one second",
			Subject:  c.Content.Attributes["graceful_timeout"].Range.Ptr(),
		}
	}

	// Create the RPC client.
	rpcTransport, err := c.transport(logger)
	if err != nil {
		return nil, err
	}
	opts := []rpc.ClientOptions{rpc.WithTransport(rpcTransport)}
	if c.EthereumKey != "" {
		key, ok := keys[c.EthereumKey]
		if !ok {
			return nil, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Validation error",
				Detail:   fmt.Sprintf("Ethereum key %q is not configured", c.EthereumKey),
				Subject:  c.Content.Attributes["ethereum_key"].Range.Ptr(),
			}
		}
		opts = append(
			opts,
			rpc.WithKeys(key),
			rpc.WithDefaultAddress(key.Address()),
		)
	}
	if c.ChainID != 0 {
		opts = append(opts, rpc.WithChainID(c.ChainID))
	}

	for _, u := range c.RPCURLs {
		logger.
			WithField("client", c.Name).
			WithField("url", u.String()).
			Info("Ethereum RPC URL")
	}

	client, err := rpc.NewClient(opts...)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Runtime error",
			Detail:   fmt.Sprintf("Failed to create the Ethereum client: %v", err),
			Subject:  c.Range.Ptr(),
		}
	}

	c.client = client
	return client, nil
}

func (c *ConfigClient) transport(logger log.Logger) (transport.Transport, error) {
	rpcURLs := make([]string, len(c.RPCURLs))
	for i, u := range c.RPCURLs {
		rpcURLs[i] = u.String()
	}
	// In theory, we don't need to use RPC-Splitter for a single endpoint, but
	// to make the application behavior consistent we use it.
	splitter, err := rpcsplitter.NewTransport(
		splitterVirtualHost,
		nil,
		rpcsplitter.WithEndpoints(rpcURLs),
		rpcsplitter.WithTotalTimeout(time.Second*time.Duration(c.Timeout)),
		rpcsplitter.WithGracefulTimeout(time.Second*time.Duration(c.GracefulTimeout)),
		rpcsplitter.WithRequirements(minimumRequiredResponses(len(c.RPCURLs)), int(c.MaxBlocksBehind)),
		rpcsplitter.WithLogger(logger),
	)
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Runtime error",
			Detail:   fmt.Sprintf("Failed to create RPC-Splitter: %v", err),
			Subject:  c.Range.Ptr(),
		}
	}
	rpcTransport, err := transport.NewHTTP(transport.HTTPOptions{
		URL:        fmt.Sprintf("http://%s", splitterVirtualHost),
		HTTPClient: &http.Client{Transport: splitter},
	})
	if err != nil {
		return nil, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Runtime error",
			Detail:   fmt.Sprintf("Failed to create the Ethereum RPC transport: %v", err),
			Subject:  c.Range.Ptr(),
		}
	}
	return rpcTransport, nil
}

func readAccountPassphrase(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	passphrase, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(passphrase), "\n"), nil
}

func minimumRequiredResponses(endpoints int) int {
	if endpoints < 2 {
		return endpoints
	}
	return endpoints - 1
}

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
