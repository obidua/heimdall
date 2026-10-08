package checkpoint

import (
	"bytes"
	"fmt"
	"math/big"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/google/uuid"
	"github.com/maticnetwork/heimdall/checkpoint/types"
	"github.com/maticnetwork/heimdall/common"
	"github.com/maticnetwork/heimdall/helper"
	hmTypes "github.com/maticnetwork/heimdall/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

var recoveryJournalPrefix = []byte{0xe0}

type RecoveryJournal struct {
	Message            types.MsgCheckpointRecovery `json:"message"`
	Height             int64                       `json:"height"`
	PreviousCheckpoint hmTypes.Checkpoint          `json:"previous_checkpoint"`
	PreviousMilestone  *hmTypes.Milestone          `json:"previous_milestone,omitempty"`
	PreviousBuffer     *hmTypes.Checkpoint         `json:"previous_buffer,omitempty"`
}

func validateRecoveryState(ctx sdk.Context, k Keeper, msg types.MsgCheckpointRecovery) error {
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	if msg.BorChainID != k.ck.GetParams(ctx).ChainParams.BorChainID {
		return fmt.Errorf("wrong Bor chain")
	}
	set := k.sk.GetValidatorSet(ctx)
	if !set.HasAddress(msg.From.Bytes()) {
		return fmt.Errorf("recovery relayer is not a current validator")
	}
	if k.GetACKCount(ctx) != msg.ExpectedACKCount || k.GetMilestoneCount(ctx) != msg.ExpectedMilestoneCount {
		return fmt.Errorf("stale recovery counters")
	}
	previous, err := k.GetLastCheckpoint(ctx)
	if err != nil {
		return err
	}
	if !previous.RootHash.Equals(msg.ExpectedCheckpointRoot) || msg.EndBlock >= previous.EndBlock {
		return fmt.Errorf("not the expected recovery branch transition")
	}
	if msg.EndBlock-msg.StartBlock+1 > k.GetParams(ctx).MaxCheckpointLength {
		return fmt.Errorf("recovery anchor exceeds checkpoint maximum")
	}
	if msg.ExpectedMilestoneCount > 0 {
		milestone, err := k.GetLastMilestone(ctx)
		if err != nil {
			return err
		}
		if !milestone.Hash.Equals(msg.ExpectedMilestoneHash) || msg.EndBlock >= milestone.EndBlock {
			return fmt.Errorf("stale milestone recovery identity")
		}
	}
	store := ctx.KVStore(k.storeKey)
	if store.Has(GetCheckpointKey(msg.Number)) || store.Has(append(append([]byte{}, recoveryJournalPrefix...), []byte(strconv.FormatUint(msg.Number, 10))...)) {
		return fmt.Errorf("recovery number already exists")
	}
	return nil
}

func handleMsgCheckpointRecovery(ctx sdk.Context, k Keeper, msg types.MsgCheckpointRecovery) sdk.Result {
	if err := validateRecoveryState(ctx, k, msg); err != nil {
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(msg.Type(), sdk.NewAttribute("number", strconv.FormatUint(msg.Number, 10))))
	return sdk.Result{Events: ctx.EventManager().Events()}
}

func SideHandleMsgCheckpointRecovery(ctx sdk.Context, k Keeper, msg types.MsgCheckpointRecovery, caller helper.IContractCaller) abci.ResponseDeliverSideTx {
	fail := func() abci.ResponseDeliverSideTx { return common.ErrorSideTx(k.Codespace(), common.CodeInvalidACK) }
	if validateRecoveryState(ctx, k, msg) != nil {
		return fail()
	}
	chain := k.ck.GetParams(ctx)
	receipt, err := caller.GetConfirmedTxReceipt(msg.TxHash.EthHash(), chain.MainchainTxConfirmations)
	if err != nil || receipt == nil || receipt.Status != ethTypes.ReceiptStatusSuccessful || receipt.BlockNumber == nil || receipt.TxHash != msg.TxHash.EthHash() {
		return fail()
	}
	canonical, err := caller.GetMainChainBlock(receipt.BlockNumber)
	if err != nil || canonical == nil || canonical.Hash() != receipt.BlockHash {
		return fail()
	}
	event, err := caller.DecodeNewHeaderBlockEvent(chain.ChainParams.RootChainAddress.EthAddress(), receipt, msg.LogIndex)
	interval := k.GetParams(ctx).ChildBlockInterval
	if err != nil || event == nil || event.HeaderBlockId == nil || event.Start == nil || event.End == nil || event.Reward == nil {
		return fail()
	}
	expectedHeader := new(big.Int).Mul(new(big.Int).SetUint64(msg.Number), new(big.Int).SetUint64(interval))
	// submitCheckpoint cannot emit a zero reward; this is the existing onlyOwner
	// administrative anchor. Verify receipt, event AND present RootChain state.
	if event.HeaderBlockId.Cmp(expectedHeader) != 0 || event.Reward.Sign() != 0 || !event.Start.IsUint64() || !event.End.IsUint64() || event.Start.Uint64() != msg.StartBlock || event.End.Uint64() != msg.EndBlock || !bytes.Equal(event.Root[:], msg.RootHash.Bytes()) || !bytes.Equal(event.Proposer.Bytes(), msg.Proposer.Bytes()) {
		return fail()
	}
	instance, err := caller.GetRootChainInstance(chain.ChainParams.RootChainAddress.EthAddress())
	if err != nil {
		return fail()
	}
	number, err := caller.CurrentHeaderBlock(instance, interval)
	if err != nil || number != msg.Number {
		return fail()
	}
	root, start, end, _, proposer, err := caller.GetHeaderInfo(msg.Number, instance, interval)
	if err != nil || start != msg.StartBlock || end != msg.EndBlock || !bytes.Equal(root.Bytes(), msg.RootHash.Bytes()) || !proposer.Equals(msg.Proposer) {
		return fail()
	}
	valid, err := types.ValidateCheckpoint(msg.StartBlock, msg.EndBlock, msg.RootHash, k.GetParams(ctx).MaxCheckpointLength, caller, chain.MaticchainTxConfirmations)
	if err != nil || !valid {
		return fail()
	}
	header, err := caller.GetMaticChainBlock(new(big.Int).SetUint64(msg.EndBlock))
	if err != nil || header == nil || header.Number == nil || header.Number.Uint64() != msg.EndBlock || !bytes.Equal(header.Hash().Bytes(), msg.EndBlockHash.Bytes()) {
		return fail()
	}
	return abci.ResponseDeliverSideTx{Result: abci.SideTxResultType_Yes}
}

func PostHandleMsgCheckpointRecovery(ctx sdk.Context, k Keeper, msg types.MsgCheckpointRecovery, result abci.SideTxResultType) sdk.Result {
	if result != abci.SideTxResultType_Yes {
		return common.ErrBadBlockDetails(k.Codespace()).Result()
	}
	if err := validateRecoveryState(ctx, k, msg); err != nil {
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}
	cached, write := ctx.CacheContext()
	previous, _ := k.GetLastCheckpoint(cached)
	previousMilestone, _ := k.GetLastMilestone(cached)
	buffer, _ := k.GetCheckpointFromBuffer(cached)
	journal := RecoveryJournal{Message: msg, Height: ctx.BlockHeight(), PreviousCheckpoint: previous, PreviousMilestone: previousMilestone, PreviousBuffer: buffer}
	encoded, err := k.cdc.MarshalJSON(journal)
	if err != nil {
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}
	checkpoint := hmTypes.Checkpoint{Proposer: msg.Proposer, StartBlock: msg.StartBlock, EndBlock: msg.EndBlock, RootHash: msg.RootHash, BorChainID: msg.BorChainID, TimeStamp: uint64(ctx.BlockTime().Unix())}
	if err := k.AddCheckpoint(cached, msg.Number, checkpoint); err != nil {
		return sdk.ErrUnknownRequest(err.Error()).Result()
	}
	// Append without pruning: every previously retained milestone record remains.
	if msg.ExpectedMilestoneCount > 0 {
		milestone := hmTypes.Milestone{Proposer: msg.Proposer, StartBlock: msg.StartBlock, EndBlock: msg.EndBlock, Hash: msg.EndBlockHash, BorChainID: msg.BorChainID, MilestoneID: uuid.NewSHA1(uuid.NameSpaceOID, msg.TxHash.Bytes()).String() + " - " + msg.Proposer.String(), TimeStamp: checkpoint.TimeStamp}
		if err := k.addMilestone(cached, GetMilestoneKey(msg.ExpectedMilestoneCount+1), milestone); err != nil {
			return sdk.ErrUnknownRequest(err.Error()).Result()
		}
		k.SetMilestoneCount(cached, msg.ExpectedMilestoneCount+1)
		k.SetMilestoneBlockNumber(cached, ctx.BlockHeight())
	}
	cached.KVStore(k.storeKey).Set(append(append([]byte{}, recoveryJournalPrefix...), []byte(strconv.FormatUint(msg.Number, 10))...), encoded)
	k.FlushCheckpointBuffer(cached)
	k.UpdateACKCountWithValue(cached, msg.Number)
	// This is an owner anchor, not a rewarded signed checkpoint: do not rotate
	// proposer priority or rewrite staking/auth/span/state-sync metadata.
	write()
	ctx.EventManager().EmitEvent(sdk.NewEvent(msg.Type(), sdk.NewAttribute("number", strconv.FormatUint(msg.Number, 10))))
	return sdk.Result{Events: ctx.EventManager().Events()}
}
