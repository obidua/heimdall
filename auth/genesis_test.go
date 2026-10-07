package auth_test

import (
	"math/rand"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/maticnetwork/heimdall/app"
	"github.com/maticnetwork/heimdall/auth"
	"github.com/maticnetwork/heimdall/auth/types"
	authTypes "github.com/maticnetwork/heimdall/auth/types"
	"github.com/maticnetwork/heimdall/types/simulation"
)

//
// Test suite
//

// GenesisTestSuite integrate test suite context object
type GenesisTestSuite struct {
	suite.Suite

	app *app.HeimdallApp
	ctx sdk.Context
}

func (suite *GenesisTestSuite) SetupTest() {
	r := rand.New(rand.NewSource(42))            // seed = 42
	accounts := simulation.RandomAccounts(r, 10) // create 10 accounts

	// genesis accounts
	var genesisAccs authTypes.GenesisAccounts

	for _, acc := range accounts {
		bacc := types.NewBaseAccountWithAddress(acc.Address)
		gacc, _ := types.NewGenesisAccountI(&bacc)
		genesisAccs = append(genesisAccs, gacc)
	}

	// app and context
	suite.app = app.SetupWithGenesisAccounts(genesisAccs)
	suite.ctx = suite.app.BaseApp.NewContext(true, abci.Header{})
}

func TestGenesisTestSuite(t *testing.T) {
	t.Parallel()
	suite.Run(t, new(GenesisTestSuite))
}

//
// Tests
//

func (suite *GenesisTestSuite) TestInitGenesis() {
	t, happ, ctx := suite.T(), suite.app, suite.ctx

	accounts := happ.AccountKeeper.GetAllAccounts(ctx)
	require.LessOrEqual(t, 10, len(accounts))
}

func (suite *GenesisTestSuite) TestExportGenesis() {
	t, happ, ctx := suite.T(), suite.app, suite.ctx

	genesisState := auth.ExportGenesis(ctx, happ.AccountKeeper)
	require.LessOrEqual(t, 10, len(genesisState.Accounts))
}

// Exported auth state must restore account numbers even when app construction
// already allocated a module account, and must retain gaps in the allocator.
func TestExportImportPreservesSigningMetadata(t *testing.T) {
	source := app.SetupWithGenesisAccounts(nil)
	sourceCtx := source.BaseApp.NewContext(true, abci.Header{})
	random := simulation.RandomAccounts(rand.New(rand.NewSource(71)), 3)
	for index := 0; index < 2; index++ {
		account := source.AccountKeeper.NewAccountWithAddress(sourceCtx, random[index].Address)
		require.NoError(t, account.SetSequence(uint64(index+7)))
		require.NoError(t, account.SetCoins(sdk.NewCoins(sdk.NewInt64Coin(authTypes.FeeToken, int64(index+123)))))
		source.AccountKeeper.SetAccount(sourceCtx, account)
	}
	// Numbers may be reserved without a surviving account record.
	source.AccountKeeper.GetNextAccountNumber(sourceCtx)
	source.AccountKeeper.GetNextAccountNumber(sourceCtx)
	exported := auth.ExportGenesis(sourceCtx, source.AccountKeeper)
	require.True(t, exported.PreserveAccountNumbers)
	require.NotNil(t, exported.NextAccountNumber)
	require.NoError(t, authTypes.ValidateGenesis(exported))

	target := app.Setup(true)
	genesis := app.NewDefaultGenesisState()
	genesis[authTypes.ModuleName] = target.Codec().MustMarshalJSON(exported)
	encoded := target.Codec().MustMarshalJSON(genesis)
	target.InitChain(abci.RequestInitChain{Validators: []abci.ValidatorUpdate{}, AppStateBytes: encoded})
	target.Commit()
	target.BeginBlock(abci.RequestBeginBlock{Header: abci.Header{Height: target.LastBlockHeight() + 1}})
	targetCtx := target.BaseApp.NewContext(true, abci.Header{})
	restored := auth.ExportGenesis(targetCtx, target.AccountKeeper)
	require.Equal(t, exported, restored)
	next := target.AccountKeeper.NewAccountWithAddress(targetCtx, random[2].Address)
	require.Equal(t, *exported.NextAccountNumber, next.GetAccountNumber())
}

func TestPreservedAuthGenesisRejectsAmbiguousNumbers(t *testing.T) {
	random := simulation.RandomAccounts(rand.New(rand.NewSource(89)), 2)
	accounts := authTypes.GenesisAccounts{}
	for _, account := range random {
		base := authTypes.NewBaseAccountWithAddress(account.Address)
		genesis, err := authTypes.NewGenesisAccountI(&base)
		require.NoError(t, err)
		accounts = append(accounts, genesis)
	}
	data := authTypes.NewGenesisState(authTypes.DefaultParams(), accounts)
	require.NoError(t, authTypes.ValidateGenesis(data), "fresh genesis still allocates default numbers")
	data.PreserveAccountNumbers = true
	require.Error(t, authTypes.ValidateGenesis(data), "allocator is required")
	next := uint64(9)
	data.NextAccountNumber = &next
	require.Error(t, authTypes.ValidateGenesis(data), "duplicate restored numbers")
	data.Accounts[1].AccountNumber = 7
	require.NoError(t, authTypes.ValidateGenesis(data))
	next = 7
	require.Error(t, authTypes.ValidateGenesis(data), "allocator would reuse a restored number")
}
