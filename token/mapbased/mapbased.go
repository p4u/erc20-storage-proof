package mapbased

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/vocdoni/storage-proofs-eth-go/ethstorageproof"
	"github.com/vocdoni/storage-proofs-eth-go/helpers"
	"github.com/vocdoni/storage-proofs-eth-go/token/erc20"
)

const (
	DiscoveryIterations = 30
)

// ErrSlotNotFound represents the storage slot not found error
var ErrSlotNotFound = errors.New("storage slot not found")

// Mapbased tokens are those where the balance is stored on a map `address => uint256`.
// Most of ERC20 tokens follows this approach.
type Mapbased struct {
	erc20 *erc20.ERC20Token
}

func (m *Mapbased) Init(tokenAddress, web3endpoint string) error {
	m.erc20 = &erc20.ERC20Token{}
	return m.erc20.Init(context.Background(), web3endpoint, tokenAddress)
}

func (m *Mapbased) GetBlock(block *big.Int) (*types.Block, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	return m.erc20.GetBlock(ctx, block)
}

// GetProof returns the storage merkle proofs for the acount holder
func (m *Mapbased) GetProof(holder common.Address,
	block *big.Int, islot int) (*ethstorageproof.StorageProof, error) {
	blockData, err := m.GetBlock(block)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	return m.getMapProofWithIndexSlot(ctx, holder, blockData, islot)
}

// getMapProofWithIndexSlot returns the storage merkle proofs for the acount holder.
// The index slot is the position on the EVM storage sub-trie for the contract.
// If index slot is unknown, GetProof() could be used instead to try to find it
func (m *Mapbased) getMapProofWithIndexSlot(ctx context.Context, holder common.Address,
	block *types.Block, islot int) (*ethstorageproof.StorageProof, error) {
	slot, err := helpers.GetMapSlot(holder.Hex(), islot)
	if err != nil {
		return nil, err
	}
	keys := []string{fmt.Sprintf("%x", slot)}
	if block == nil {
		block, err = m.erc20.GetBlock(ctx, nil)
		if err != nil {
			return nil, err
		}
		if block == nil {
			return nil, fmt.Errorf("cannot fetch block info")
		}
	}
	return m.erc20.GetProof(ctx, keys, block)
}

// DiscoverSlot tries to find the EVM storage index slot.
// A token holder address must be provided in order to have a balance to search and compare.
// Returns ErrSlotNotFound if the slot cannot be found.
// If found, returns also the amount stored.
func (m *Mapbased) DiscoverSlot(holder common.Address) (int, *big.Float, error) {
	var slot [32]byte
	tokenData, err := m.erc20.GetTokenData()
	if err != nil {
		return -1, nil, fmt.Errorf("GetTokenData: %w", err)
	}
	balance, err := m.erc20.Balance(holder)
	if err != nil {
		return -1, nil, fmt.Errorf("Balance: %w", err)
	}

	addr := common.Address{}
	copy(addr[:], m.erc20.TokenAddr[:20])

	ubalance, _ := balance.Uint64()
	amount := big.NewFloat(0)
	index := -1
	for i := 0; i < DiscoveryIterations; i++ {
		// Prepare storage index
		slot, err = helpers.GetMapSlot(holder.Hex(), i)
		if err != nil {
			return index, nil, fmt.Errorf("GetSlot: %w", err)
		}
		// Get Storage
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		value, err := m.erc20.Ethcli.StorageAt(ctx, addr, slot, nil)
		cancel()
		if err != nil {
			return index, nil, err
		}

		// Parse balance value
		amount, err := helpers.ValueToBalance(fmt.Sprintf("%x", value), int(tokenData.Decimals))
		if err != nil {
			continue
		}
		// Check if balance matches
		if a, _ := amount.Uint64(); a == ubalance {
			index = i
			break
		}
	}
	if index == -1 {
		return index, nil, ErrSlotNotFound
	}
	return index, amount, nil
}
