package checkpoint_test

import (
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethCommon "github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/maticnetwork/heimdall/app"
	"github.com/maticnetwork/heimdall/checkpoint"
	chSim "github.com/maticnetwork/heimdall/checkpoint/simulation"
	chTypes "github.com/maticnetwork/heimdall/checkpoint/types"
	"github.com/maticnetwork/heimdall/contracts/rootchain"
	"github.com/maticnetwork/heimdall/helper/mocks"
	hmTypes "github.com/maticnetwork/heimdall/types"
)

func recoveryFixture(t *testing.T) (*app.HeimdallApp, sdk.Context, chTypes.MsgCheckpointRecovery) {
	a, ctx, _ := createTestApp(false)
	ctx = ctx.WithBlockHeader(abci.Header{Height: 14226235, Time: time.Unix(1790000000, 0)})
	chain := a.ChainKeeper.GetParams(ctx)
	chain.ChainParams.BorChainID = "1370"
	a.ChainKeeper.SetParams(ctx, chain)
	chSim.LoadValidatorSet(t, 2, a.StakingKeeper, ctx, false, 10)
	set := a.StakingKeeper.GetValidatorSet(ctx)
	actor := set.GetProposer().Signer
	old := hmTypes.Checkpoint{Proposer: actor, StartBlock: 1000, EndBlock: 1255, RootHash: hmTypes.HexToHeimdallHash("11"), BorChainID: "1370", TimeStamp: 1780000000}
	require.NoError(t, a.CheckpointKeeper.AddCheckpoint(ctx, 1, old))
	a.CheckpointKeeper.UpdateACKCountWithValue(ctx, 1)
	milestone := hmTypes.Milestone{Proposer: actor, StartBlock: 1256, EndBlock: 1275, Hash: hmTypes.HexToHeimdallHash("22"), BorChainID: "1370", MilestoneID: "legacy", TimeStamp: 1780000001}
	require.NoError(t, a.CheckpointKeeper.AddMilestone(ctx, milestone))
	buffer := old
	buffer.StartBlock = 1256
	buffer.EndBlock = 1511
	require.NoError(t, a.CheckpointKeeper.SetCheckpointBuffer(ctx, buffer))
	msg := chTypes.MsgCheckpointRecovery{From: actor, Proposer: actor, Number: 2, ExpectedACKCount: 1, ExpectedCheckpointRoot: old.RootHash, ExpectedMilestoneCount: 1, ExpectedMilestoneHash: milestone.Hash, StartBlock: 600, EndBlock: 855, RootHash: hmTypes.HexToHeimdallHash("33"), EndBlockHash: hmTypes.HexToHeimdallHash("44"), BorChainID: "1370", TxHash: hmTypes.HexToHeimdallHash("55")}
	return a, ctx, msg
}

func TestRecoveryPreservesRecordsRejectsReplayAndRequiresSideApproval(t *testing.T) {
	a, ctx, msg := recoveryFixture(t)
	old, _ := a.CheckpointKeeper.GetLastCheckpoint(ctx)
	milestone, _ := a.CheckpointKeeper.GetLastMilestone(ctx)
	beforeHeight := ctx.BlockHeight()
	beforeStaking := a.Codec().MustMarshalJSON(a.StakingKeeper.GetValidatorSet(ctx))
	handler := checkpoint.NewHandler(a.CheckpointKeeper, &mocks.IContractCaller{})
	require.True(t, handler(ctx, msg).IsOK())
	require.False(t, checkpoint.PostHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, abci.SideTxResultType_No).IsOK())
	require.Equal(t, uint64(1), a.CheckpointKeeper.GetACKCount(ctx))
	require.True(t, checkpoint.PostHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, abci.SideTxResultType_Yes).IsOK())
	retained, err := a.CheckpointKeeper.GetCheckpointByNumber(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, old, retained)
	retainedMilestone, err := a.CheckpointKeeper.GetMilestoneByNumber(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, milestone, retainedMilestone)
	latest, err := a.CheckpointKeeper.GetLastCheckpoint(ctx)
	require.NoError(t, err)
	require.Equal(t, msg.EndBlock, latest.EndBlock)
	newestMilestone, err := a.CheckpointKeeper.GetLastMilestone(ctx)
	require.NoError(t, err)
	require.Equal(t, msg.EndBlockHash, newestMilestone.Hash)
	require.Equal(t, beforeHeight, ctx.BlockHeight())
	require.Equal(t, beforeStaking, a.Codec().MustMarshalJSON(a.StakingKeeper.GetValidatorSet(ctx)))
	_, err = a.CheckpointKeeper.GetCheckpointFromBuffer(ctx)
	require.Error(t, err)
	require.False(t, handler(ctx, msg).IsOK(), "replayed transition must fail")
	require.False(t, checkpoint.PostHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, abci.SideTxResultType_Yes).IsOK())
}

func TestRecoveryRejectsWrongStateAndUnauthorizedRelayer(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*chTypes.MsgCheckpointRecovery)
	}{
		{"unregistered relayer", func(m *chTypes.MsgCheckpointRecovery) { m.From = hmTypes.HexToHeimdallAddress("999") }},
		{"wrong previous checkpoint", func(m *chTypes.MsgCheckpointRecovery) { m.ExpectedCheckpointRoot = hmTypes.HexToHeimdallHash("999") }},
		{"wrong previous milestone", func(m *chTypes.MsgCheckpointRecovery) { m.ExpectedMilestoneHash = hmTypes.HexToHeimdallHash("999") }},
		{"wrong sequence", func(m *chTypes.MsgCheckpointRecovery) { m.Number = 3 }},
		{"wrong chain", func(m *chTypes.MsgCheckpointRecovery) { m.BorChainID = "1" }},
		{"forward normal checkpoint", func(m *chTypes.MsgCheckpointRecovery) { m.StartBlock = 1300; m.EndBlock = 1555 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			a, ctx, msg := recoveryFixture(t)
			change.apply(&msg)
			require.False(t, checkpoint.NewHandler(a.CheckpointKeeper, &mocks.IContractCaller{})(ctx, msg).IsOK())
			require.Equal(t, uint64(1), a.CheckpointKeeper.GetACKCount(ctx))
		})
	}
}

func TestRecoverySideValidatesConfirmedOwnerEventParentCursorAndBorBranch(t *testing.T) {
	for _, failure := range []string{"", "failed receipt", "wrong parent hash", "rewarded event", "wrong parent cursor", "wrong Bor root", "wrong end-block hash"} {
		t.Run(failure, func(t *testing.T) {
			a, ctx, msg := recoveryFixture(t)
			caller := &mocks.IContractCaller{}
			parentHeader := &ethTypes.Header{Number: big.NewInt(95127300), Time: 1790000000}
			receipt := &ethTypes.Receipt{Status: ethTypes.ReceiptStatusSuccessful, TxHash: msg.TxHash.EthHash(), BlockNumber: parentHeader.Number, BlockHash: parentHeader.Hash()}
			if failure == "failed receipt" {
				receipt.Status = ethTypes.ReceiptStatusFailed
			}
			if failure == "wrong parent hash" {
				receipt.BlockHash = ethCommon.HexToHash("999")
			}
			caller.On("GetConfirmedTxReceipt", msg.TxHash.EthHash(), a.ChainKeeper.GetParams(ctx).MainchainTxConfirmations).Return(receipt, nil)
			caller.On("GetMainChainBlock", mock.Anything).Return(parentHeader, nil)
			params := a.CheckpointKeeper.GetParams(ctx)
			event := &rootchain.RootchainNewHeaderBlock{Proposer: msg.Proposer.EthAddress(), HeaderBlockId: new(big.Int).SetUint64(msg.Number * params.ChildBlockInterval), Reward: big.NewInt(0), Start: new(big.Int).SetUint64(msg.StartBlock), End: new(big.Int).SetUint64(msg.EndBlock)}
			copy(event.Root[:], msg.RootHash.Bytes())
			if failure == "rewarded event" {
				event.Reward = big.NewInt(1)
			}
			caller.On("DecodeNewHeaderBlockEvent", mock.Anything, receipt, msg.LogIndex).Return(event, nil)
			instance := &rootchain.Rootchain{}
			caller.On("GetRootChainInstance", mock.Anything).Return(instance, nil)
			number := msg.Number
			if failure == "wrong parent cursor" {
				number++
			}
			caller.On("CurrentHeaderBlock", instance, params.ChildBlockInterval).Return(number, nil)
			caller.On("GetHeaderInfo", msg.Number, instance, params.ChildBlockInterval).Return(msg.RootHash.EthHash(), msg.StartBlock, msg.EndBlock, uint64(1790000000), msg.Proposer, nil)
			caller.On("CheckIfBlocksExist", mock.Anything).Return(true)
			root := msg.RootHash.Bytes()
			if failure == "wrong Bor root" {
				root = []byte{1}
			}
			caller.On("GetRootHash", msg.StartBlock, msg.EndBlock, params.MaxCheckpointLength).Return(root, nil)
			childHeader := &ethTypes.Header{Number: new(big.Int).SetUint64(msg.EndBlock), Time: 1790000000}
			if failure != "wrong end-block hash" {
				msg.EndBlockHash = hmTypes.BytesToHeimdallHash(childHeader.Hash().Bytes())
			}
			caller.On("GetMaticChainBlock", mock.Anything).Return(childHeader, nil)
			result := checkpoint.SideHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, caller)
			if failure == "" {
				require.Equal(t, abci.SideTxResultType_Yes, result.Result)
			} else {
				require.NotEqual(t, abci.SideTxResultType_Yes, result.Result)
			}
		})
	}
}
