package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/maticnetwork/heimdall/checkpoint"
	chTypes "github.com/maticnetwork/heimdall/checkpoint/types"
	"github.com/maticnetwork/heimdall/helper"
)

// Explicitly opt-in. Opens a SECOND copy of the already isolated application DB.
// Never commits, exports/imports genesis, signs or broadcasts a transaction.
func TestRecoveryApplicationCopy(t *testing.T) {
	if os.Getenv("RAMESTTA_COPY_AUDIT") != "1" {
		t.Skip("isolated Ravi application copy only")
	}
	const base = "/srv/ramestta-recovery/rehearsal/inplace-audit"
	require.Equal(t, base, filepath.Clean(os.Getenv("RAMESTTA_COPY_AUDIT_HOME")))
	viper.Set(helper.MainRPCUrlFlag, "http://127.0.0.1:18549")
	viper.Set(helper.BorRPCUrlFlag, "http://127.0.0.1:18545")
	helper.InitHeimdallConfig("/srv/ramestta-recovery/heimdall")
	db := dbm.NewDB("application", dbm.GoLevelDBBackend, base)
	defer db.Close()
	a := NewHeimdallApp(log.NewNopLogger(), db)
	height := a.LastBlockHeight()
	require.Greater(t, height, int64(14000000))
	ctx := a.NewContext(true, abci.Header{Height: height, Time: time.Unix(1791442800, 0)})
	previous, err := a.CheckpointKeeper.GetLastCheckpoint(ctx)
	require.NoError(t, err)
	milestone, err := a.CheckpointKeeper.GetLastMilestone(ctx)
	require.NoError(t, err)
	set := a.StakingKeeper.GetValidatorSet(ctx)
	manifest := map[string]interface{}{"height": height, "checkpoint": previous, "ackCount": a.CheckpointKeeper.GetACKCount(ctx), "milestone": milestone, "milestoneCount": a.CheckpointKeeper.GetMilestoneCount(ctx), "validators": set, "chainParams": a.ChainKeeper.GetParams(ctx), "checkpointParams": a.CheckpointKeeper.GetParams(ctx)}
	data, err := json.MarshalIndent(manifest, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(base+"/source-manifest.json", data, 0600))
	if os.Getenv("RAMESTTA_COPY_AUDIT_APPLY") != "1" {
		return
	}
	raw, err := os.ReadFile(base + "/message.json")
	require.NoError(t, err)
	var msg chTypes.MsgCheckpointRecovery
	require.NoError(t, json.Unmarshal(raw, &msg))
	snapshot := func(c sdk.Context) map[string]map[string][]byte {
		all := map[string]map[string][]byte{}
		for name, key := range a.keys {
			all[name] = map[string][]byte{}
			it := c.KVStore(key).Iterator(nil, nil)
			for ; it.Valid(); it.Next() {
				all[name][string(it.Key())] = append([]byte{}, it.Value()...)
			}
			it.Close()
		}
		return all
	}
	before := snapshot(ctx)
	caller, err := helper.NewContractCaller()
	require.NoError(t, err)
	require.True(t, checkpoint.NewHandler(a.CheckpointKeeper, &caller)(ctx, msg).IsOK())
	side := checkpoint.SideHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, &caller)
	require.Equal(t, abci.SideTxResultType_Yes, side.Result, "real parent receipt and copied Bor must validate")
	require.True(t, checkpoint.PostHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, side.Result).IsOK())
	after := snapshot(ctx)
	changed := map[string][]string{}
	hashes := map[string]string{}
	for name, entries := range before {
		for key, value := range entries {
			if !bytes.Equal(value, after[name][key]) {
				changed[name] = append(changed[name], hex.EncodeToString([]byte(key)))
				require.Equal(t, chTypes.StoreKey, name, "non-checkpoint store changed")
				require.Contains(t, []string{"11", "12", "30", "70"}, hex.EncodeToString([]byte(key)), "historical checkpoint/milestone record changed")
			}
		}
		for key := range after[name] {
			if _, ok := entries[key]; !ok {
				require.Equal(t, chTypes.StoreKey, name, "new non-checkpoint key")
			}
		}
		h := sha256.New()
		it := ctx.KVStore(a.keys[name]).Iterator(nil, nil)
		for ; it.Valid(); it.Next() {
			var n [8]byte
			binary.BigEndian.PutUint64(n[:], uint64(len(it.Key())))
			h.Write(n[:])
			h.Write(it.Key())
			binary.BigEndian.PutUint64(n[:], uint64(len(it.Value())))
			h.Write(n[:])
			h.Write(it.Value())
		}
		it.Close()
		hashes[name] = hex.EncodeToString(h.Sum(nil))
	}
	require.Equal(t, height, a.LastBlockHeight())
	require.False(t, checkpoint.PostHandleMsgCheckpointRecovery(ctx, a.CheckpointKeeper, msg, side.Result).IsOK(), "replay")
	report := map[string]interface{}{"directHandlerOnApplicationCopy": true, "nativeConsensusBroadcastTested": false, "mainnetReady": false, "heightPreserved": height, "allNonCheckpointStoresUnchanged": true, "historicalRecordsPreserved": true, "realParentReceiptAndBorVerified": true, "changedExistingKeys": changed, "afterStoreDigests": hashes, "message": msg}
	data, err = json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(base+"/audit-result.json", data, 0600))
}
