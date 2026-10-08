package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/client/context"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/cobra"

	"github.com/maticnetwork/heimdall/checkpoint/types"
	"github.com/maticnetwork/heimdall/helper"
)

// SendCheckpointRecovery submits a reviewed, receipt-bound transition. The file
// includes the exact previous state identity and actual parent transaction hash.
func SendCheckpointRecovery(cdc *codec.Codec) *cobra.Command {
	return &cobra.Command{
		Use:   "checkpoint-recovery message.json",
		Short: "Submit a parent-authorized checkpoint recovery for validator side voting",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var msg types.MsgCheckpointRecovery
			if err = json.Unmarshal(data, &msg); err != nil {
				return err
			}
			if err = msg.ValidateBasic(); err != nil {
				return err
			}
			cliCtx := context.NewCLIContext().WithCodec(cdc)
			if !helper.GetFromAddress(cliCtx).Equals(msg.From) {
				return fmt.Errorf("recovery sender does not match signing account")
			}
			return helper.BroadcastMsgsWithCLI(cliCtx, []sdk.Msg{msg})
		},
	}
}
