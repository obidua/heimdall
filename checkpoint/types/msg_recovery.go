package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	hmTypes "github.com/maticnetwork/heimdall/types"
	"math"
)

// MsgCheckpointRecovery requests an additive parent-authorized transition.
// Ordinary checkpoint/ACK semantics are unchanged. Side voting must verify the
// finalized zero-reward RootChain owner anchor and the surviving Bor branch.
type MsgCheckpointRecovery struct {
	From                   hmTypes.HeimdallAddress `json:"from"`
	Proposer               hmTypes.HeimdallAddress `json:"proposer"`
	Number                 uint64                  `json:"number"`
	ExpectedACKCount       uint64                  `json:"expected_ack_count"`
	ExpectedCheckpointRoot hmTypes.HeimdallHash    `json:"expected_checkpoint_root"`
	ExpectedMilestoneCount uint64                  `json:"expected_milestone_count"`
	ExpectedMilestoneHash  hmTypes.HeimdallHash    `json:"expected_milestone_hash"`
	StartBlock             uint64                  `json:"start_block"`
	EndBlock               uint64                  `json:"end_block"`
	RootHash               hmTypes.HeimdallHash    `json:"root_hash"`
	EndBlockHash           hmTypes.HeimdallHash    `json:"end_block_hash"`
	BorChainID             string                  `json:"bor_chain_id"`
	TxHash                 hmTypes.HeimdallHash    `json:"tx_hash"`
	LogIndex               uint64                  `json:"log_index"`
}

func (msg MsgCheckpointRecovery) Route() string { return RouterKey }
func (msg MsgCheckpointRecovery) Type() string  { return "checkpoint-recovery" }
func (msg MsgCheckpointRecovery) GetSigners() []sdk.AccAddress {
	return []sdk.AccAddress{hmTypes.HeimdallAddressToAccAddress(msg.From)}
}
func (msg MsgCheckpointRecovery) GetSignBytes() []byte {
	return sdk.MustSortJSON(ModuleCdc.MustMarshalJSON(msg))
}
func (msg MsgCheckpointRecovery) GetSideSignBytes() []byte { return nil }
func (msg MsgCheckpointRecovery) ValidateBasic() sdk.Error {
	if msg.From.Empty() || msg.Proposer.Empty() || msg.RootHash.Empty() || msg.EndBlockHash.Empty() || msg.TxHash.Empty() || msg.ExpectedCheckpointRoot.Empty() || msg.BorChainID == "" {
		return sdk.ErrInvalidAddress("incomplete recovery authorization/state")
	}
	if msg.ExpectedACKCount == 0 || msg.ExpectedACKCount == math.MaxUint64 || msg.Number != msg.ExpectedACKCount+1 || msg.ExpectedMilestoneCount == math.MaxUint64 || msg.StartBlock >= msg.EndBlock || msg.EndBlock > math.MaxUint64-1024 {
		return sdk.ErrUnknownRequest("invalid recovery sequence/range")
	}
	if msg.ExpectedMilestoneCount > 0 && msg.ExpectedMilestoneHash.Empty() {
		return sdk.ErrUnknownRequest("missing previous milestone identity")
	}
	return nil
}
